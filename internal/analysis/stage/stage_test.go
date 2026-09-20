package stage_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
)

type scorerFunc func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error)

func (f scorerFunc) Score(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
	return f(ctx, file, debug, candidates)
}

func fixedScores(scores ...float64) stage.Scorer {
	return scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		return scores, nil
	})
}

type fakeFile struct{ path, text string }

func (f fakeFile) Path() string      { return f.path }
func (f fakeFile) QueryText() string { return f.text }

func candidates(ids ...string) []stage.Candidate {
	out := make([]stage.Candidate, len(ids))
	for i, id := range ids {
		out[i] = stage.Candidate{ADR: &index.ADR{ID: id, Title: "ADR " + id}}
	}
	return out
}

func ids(cs []stage.Candidate) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ADR.ID
	}
	return strings.Join(out, ",")
}

func TestStage_DropsBelowMinimumAndOrdersBestFirst(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(0.4, 0.9, 0.2, 0.7), Min: stage.FixedMin(0.3)}

	got, err := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, candidates("a", "b", "c", "d"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ids(got) != "b,d,a" {
		t.Fatalf("got %s, want b,d,a", ids(got))
	}
	if got[0].Score != 0.9 {
		t.Fatalf("survivor score = %v, want the scorer's 0.9 recorded on the candidate", got[0].Score)
	}
}

func TestStage_MaxKeepCutsAfterOrdering(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(0.4, 0.9, 0.7), Min: stage.FixedMin(0), MaxKeep: 2}

	got, _ := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, candidates("a", "b", "c"))
	if ids(got) != "b,c" {
		t.Fatalf("got %s, want b,c", ids(got))
	}
}

func TestStage_NilMinDefaultsToZero(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(-0.5, 0)}

	got, _ := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, candidates("a", "b"))
	if ids(got) != "b" {
		t.Fatalf("got %s, want only b (score 0 kept, negative dropped)", ids(got))
	}
}

func TestStage_PerADRThreshold(t *testing.T) {
	strict := 0.95
	cs := candidates("a", "b")
	cs[0].ADR.SimilarityThreshold = &strict
	s := stage.Stage{Scorer: fixedScores(0.9, 0.9), Min: stage.ADRThreshold{Global: 0.5}}

	got, _ := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, cs)
	if ids(got) != "b" {
		t.Fatalf("got %s, want only b (a's own 0.95 threshold rejects 0.9)", ids(got))
	}
}

func TestStage_DebugReportsBelowThresholdAndTopKCut(t *testing.T) {
	var buf bytes.Buffer
	s := stage.Stage{Scorer: fixedScores(0.9, 0.8, 0.7, 0.1), Min: stage.FixedMin(0.5), MaxKeep: 2}

	if _, err := s.Apply(context.Background(), fakeFile{}, stage.NewDebug(&buf), candidates("a", "b", "c", "d")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Below threshold: ADR d (score 0.10 < threshold 0.50)") {
		t.Errorf("missing below-threshold line in %q", out)
	}
	if !strings.Contains(out, "Cut by top-K limit: ADR c (score 0.70, rank 3 of 3 qualifying ADRs)") {
		t.Errorf("missing top-K cut line in %q", out)
	}
}

func TestStage_DebugBelowThresholdLinesAreCappedAtMaxKeep(t *testing.T) {
	var buf bytes.Buffer
	s := stage.Stage{Scorer: fixedScores(0.1, 0.2, 0.3, 0.4), Min: stage.FixedMin(0.5), MaxKeep: 2}

	_, _ = s.Apply(context.Background(), fakeFile{}, stage.NewDebug(&buf), candidates("a", "b", "c", "d"))
	if n := strings.Count(buf.String(), "Below threshold"); n != 2 {
		t.Fatalf("printed %d below-threshold lines, want 2", n)
	}
}

func TestStage_NoDebugPrintsNothing(t *testing.T) {
	if stage.NoDebug.Enabled() {
		t.Fatal("NoDebug reports enabled")
	}
	stage.NoDebug.Printf("dropped %d", 1)
}

func TestStage_WrapsScorerErrorsWithAScoringAction(t *testing.T) {
	boom := errors.New("boom")
	s := stage.Stage{Scorer: scorerFunc(func(context.Context, stage.File, stage.Debug, []stage.Candidate) ([]float64, error) {
		return nil, boom
	})}

	_, err := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, candidates("a"))

	var stageErr *stage.Error
	if !errors.As(err, &stageErr) || stageErr.Action != "scoring candidates" || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want a *stage.Error with action scoring candidates wrapping boom", err)
	}
}

func TestStage_KeepsAScorersOwnErrorAction(t *testing.T) {
	own := &stage.Error{Action: "screening", Err: errors.New("boom")}
	s := stage.Stage{Scorer: scorerFunc(func(context.Context, stage.File, stage.Debug, []stage.Candidate) ([]float64, error) {
		return nil, own
	})}

	_, err := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, candidates("a"))
	if err != error(own) {
		t.Fatalf("err = %v, want the scorer's own *stage.Error passed through unchanged", err)
	}
}

func TestStage_RejectsWrongScoreCount(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(1)}

	_, err := s.Apply(context.Background(), fakeFile{}, stage.NoDebug, candidates("a", "b"))

	var stageErr *stage.Error
	if !errors.As(err, &stageErr) || stageErr.Action != "scoring candidates" {
		t.Fatalf("err = %v, want a scoring-candidates *stage.Error for 1 score across 2 candidates", err)
	}
}
