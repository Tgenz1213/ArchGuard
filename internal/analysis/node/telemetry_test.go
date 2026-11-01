package node_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis/node"
)

func slowScorer(d time.Duration, scores ...float64) node.Scorer {
	inner := fixedScores(scores...)
	return scorerFunc(func(ctx context.Context, file node.File, debug node.Debug, candidates []node.Candidate) ([]float64, error) {
		time.Sleep(d)
		return inner.Score(ctx, file, debug, candidates)
	})
}

func TestTelemetry_SumsReceivedKeptAndTimeAcrossCalls(t *testing.T) {
	tel := node.NewTelemetry([]node.Node{
		{Name: "rank", Scorer: slowScorer(5*time.Millisecond, 0.9, 0.8, 0.1), Min: node.FixedMin(0.5)},
		{Name: "rerank", Scorer: fixedScores(0.9, 0.8), MaxKeep: 1},
	})

	for range 2 {
		kept, err := tel.Apply(t.Context(), 0, fakeFile{}, node.NoDebug, candidates("a", "b", "c"))
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}

		if _, err := tel.Apply(t.Context(), 1, fakeFile{}, node.NoDebug, kept); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	got := tel.Stats()
	if len(got) != 2 {
		t.Fatalf("stats = %+v, want 2 nodes", got)
	}

	if got[0].Name != "rank" || got[0].Received != 6 || got[0].Kept != 4 {
		t.Errorf("rank = %+v, want received 6 kept 4", got[0])
	}

	if got[1].Name != "rerank" || got[1].Received != 4 || got[1].Kept != 2 {
		t.Errorf("rerank = %+v, want received 4 kept 2", got[1])
	}

	if got[0].DurationMS < 10 {
		t.Errorf("rank duration_ms = %d, want at least the two calls' 5ms sleeps summed", got[0].DurationMS)
	}
}

func TestTelemetry_ListsEveryNodeWithZerosBeforeAnyCall(t *testing.T) {
	tel := node.NewTelemetry([]node.Node{{Name: "rank"}, {Name: "rerank"}})

	got := tel.Stats()
	if len(got) != 2 || got[0] != (node.Stats{Name: "rank"}) || got[1] != (node.Stats{Name: "rerank"}) {
		t.Fatalf("stats = %+v, want both nodes listed with zeros", got)
	}
}

func TestTelemetry_NodeThatReceivesNothingReportsZeros(t *testing.T) {
	tel := node.NewTelemetry([]node.Node{{Name: "rank", Scorer: fixedScores()}})

	if _, err := tel.Apply(t.Context(), 0, fakeFile{}, node.NoDebug, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := tel.Stats()[0]; got.Received != 0 || got.Kept != 0 {
		t.Fatalf("stats = %+v, want zero received and kept", got)
	}
}

type slowDebug struct{ delay time.Duration }

func (d slowDebug) DebugEnabled() bool { return true }

func (d slowDebug) Debug(string, ...any) { time.Sleep(d.delay) }

func TestTelemetry_EmptyInputAddsNoTimeEvenWhenDebugOutputIsSlow(t *testing.T) {
	tel := node.NewTelemetry([]node.Node{{Name: "rank", Scorer: fixedScores()}})

	if _, err := tel.Apply(t.Context(), 0, fakeFile{}, slowDebug{delay: 5 * time.Millisecond}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := tel.Stats()[0]; got.DurationMS != 0 {
		t.Fatalf("stats = %+v, want duration_ms 0 for a node that received nothing", got)
	}
}

func TestTelemetry_FailedNodeCountsReceivedAndTimeButKeepsNothing(t *testing.T) {
	boom := errors.New("boom")
	tel := node.NewTelemetry([]node.Node{{Name: "rank", Scorer: scorerFunc(func(context.Context, node.File, node.Debug, []node.Candidate) ([]float64, error) {
		time.Sleep(3 * time.Millisecond)
		return nil, boom
	})}})

	kept, err := tel.Apply(t.Context(), 0, fakeFile{}, node.NoDebug, candidates("a", "b"))
	if !errors.Is(err, boom) || len(kept) != 0 {
		t.Fatalf("kept=%v err=%v, want the scorer's error and nothing kept", kept, err)
	}

	if got := tel.Stats()[0]; got.Received != 2 || got.Kept != 0 || got.DurationMS < 3 {
		t.Fatalf("stats = %+v, want received 2, kept 0, duration of at least 3ms", got)
	}
}

func TestTelemetry_StatsReturnsACopy(t *testing.T) {
	tel := node.NewTelemetry([]node.Node{{Name: "rank", Scorer: fixedScores(1)}})

	first := tel.Stats()
	first[0].Kept = 99

	if _, err := tel.Apply(t.Context(), 0, fakeFile{}, node.NoDebug, candidates("a")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := tel.Stats()[0]; got.Received != 1 || got.Kept != 1 {
		t.Fatalf("stats = %+v after the caller edited an earlier result, want received 1 kept 1", got)
	}
}

func TestTelemetry_ConcurrentCallsDoNotLoseCounts(t *testing.T) {
	tel := node.NewTelemetry([]node.Node{{Name: "rank", Scorer: fixedScores(1, 1)}})

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := tel.Apply(t.Context(), 0, fakeFile{}, node.NoDebug, candidates("a", "b")); err != nil {
				t.Errorf("Apply: %v", err)
			}
		})
	}

	wg.Wait()

	if got := tel.Stats()[0]; got.Received != 100 || got.Kept != 100 {
		t.Fatalf("stats = %+v, want received 100 kept 100", got)
	}
}
