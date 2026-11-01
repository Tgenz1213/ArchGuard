package stage_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
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

func (f fakeFile) Path() string                     { return f.path }
func (f fakeFile) QueryText(context.Context) string { return f.text }
func (f fakeFile) Sides() []string                  { return []string{f.text} }

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

	got, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a", "b", "c", "d"))
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

	got, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a", "b", "c"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "b,c" {
		t.Fatalf("got %s, want b,c", ids(got))
	}
}

func TestStage_NilMinDefaultsToZero(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(-0.5, 0)}

	got, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a", "b"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "b" {
		t.Fatalf("got %s, want only b (score 0 kept, negative dropped)", ids(got))
	}
}

func TestStage_PerADRThreshold(t *testing.T) {
	strict := 0.95
	cs := candidates("a", "b")
	cs[0].ADR.SimilarityThreshold = &strict
	s := stage.Stage{Scorer: fixedScores(0.9, 0.9), Min: stage.ADRThreshold{Global: 0.5}}

	got, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, cs)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "b" {
		t.Fatalf("got %s, want only b (a's own 0.95 threshold rejects 0.9)", ids(got))
	}
}

func TestStage_DoesNotScoreWhenThereAreNoCandidates(t *testing.T) {
	called := false
	s := stage.Stage{Scorer: scorerFunc(func(context.Context, stage.File, stage.Debug, []stage.Candidate) ([]float64, error) {
		called = true
		return nil, errors.New("scorer must not run without candidates")
	})}

	got, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, nil)
	if err != nil || len(got) != 0 || called {
		t.Fatalf("got=%v err=%v called=%v, want an empty result and no scorer call", got, err, called)
	}
}

func TestStage_NoDebugPrintsNothing(t *testing.T) {
	if stage.NoDebug.DebugEnabled() {
		t.Fatal("NoDebug reports enabled")
	}

	stage.NoDebug.Debug("dropped %d", 1)
}

func TestStage_WrapsScorerErrorsWithAScoringAction(t *testing.T) {
	boom := errors.New("boom")
	s := stage.Stage{Scorer: scorerFunc(func(context.Context, stage.File, stage.Debug, []stage.Candidate) ([]float64, error) {
		return nil, boom
	})}

	_, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a"))

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

	_, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a"))
	if err != error(own) { //nolint:errorlint // identity check: the error must pass through unwrapped
		t.Fatalf("err = %v, want the scorer's own *stage.Error passed through unchanged", err)
	}
}

func TestStage_RejectsWrongScoreCount(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(1)}

	_, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a", "b"))

	var stageErr *stage.Error
	if !errors.As(err, &stageErr) || stageErr.Action != "scoring candidates" {
		t.Fatalf("err = %v, want a scoring-candidates *stage.Error for 1 score across 2 candidates", err)
	}
}

func TestStage_DebugReportOfRejectedCandidates(t *testing.T) {
	perADR := 0.6
	strict := candidates("a", "b", "c", "d", "e", "f")
	strict[5].ADR.SimilarityThreshold = &perADR

	tests := []struct {
		name         string
		listRejected bool
		want         []string
		absent       []string
	}{
		{
			name: "summarized per reason",
			want: []string{
				"Kept: ADR a (score 0.90)",
				"Kept: ADR b (score 0.80)",
				"Below threshold: 2 ADR(s), scores 0.20 to 0.50 (thresholds 0.55 to 0.60)",
				"Cut by top-K limit: 2 ADR(s), scores 0.60 to 0.70 (limit 2)",
			},
			absent: []string{"ADR c", "ADR d", "ADR e", "ADR f"},
		},
		{
			name:         "listed one by one",
			listRejected: true,
			want: []string{
				"Kept: ADR a (score 0.90)",
				"Below threshold: ADR e (score 0.20 < threshold 0.55)",
				"Below threshold: ADR f (score 0.50 < threshold 0.60)",
				"Cut by top-K limit: ADR c (score 0.70, rank 3 of 4 qualifying ADRs)",
				"Cut by top-K limit: ADR d (score 0.60, rank 4 of 4 qualifying ADRs)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stage.Stage{
				Name:         "rank",
				Scorer:       fixedScores(0.9, 0.8, 0.7, 0.6, 0.2, 0.5),
				Min:          stage.ADRThreshold{Global: 0.55},
				MaxKeep:      2,
				ListRejected: tt.listRejected,
			}
			var buf bytes.Buffer

			if _, err := s.Apply(t.Context(), fakeFile{}, output.New(&buf, true), strict); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, line := range tt.want {
				if !strings.Contains(buf.String(), line) {
					t.Errorf("debug output lacks %q:\n%s", line, buf.String())
				}
			}

			for _, title := range tt.absent {
				if strings.Contains(buf.String(), title) {
					t.Errorf("debug output names rejected %q:\n%s", title, buf.String())
				}
			}
		})
	}
}

func TestStage_KeepsCandidatesScoredInfOutsideMaxKeep(t *testing.T) {
	s := stage.Stage{Name: "screen", Scorer: fixedScores(0.9, math.Inf(1), 0.8), Min: stage.FixedMin(0.5), MaxKeep: 1}
	var buf bytes.Buffer

	got, err := s.Apply(t.Context(), fakeFile{}, output.New(&buf, true), candidates("a", "b", "c"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "a,b" {
		t.Errorf("kept %s, want a (best scored) and b (not scored, outside MaxKeep)", ids(got))
	}

	if !strings.Contains(buf.String(), "Kept: ADR b (not scored by this stage)") {
		t.Errorf("debug output %q does not mark b as not scored", buf.String())
	}
}

func TestStage_ScorerErrorUnderPassThroughOnSkip(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name        string
		ctx         context.Context
		passThrough bool
		failOn      bool
		skipped     bool
	}{
		{"passes every candidate on", t.Context(), true, false, true},
		{"fails under on_error fail", t.Context(), true, true, false},
		{"fails without PassThroughOnSkip", t.Context(), false, false, false},
		{"fails once the run is cancelled", cancelled, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stage.Stage{PassThroughOnSkip: tt.passThrough, FailOnError: tt.failOn, Scorer: scorerFunc(func(context.Context, stage.File, stage.Debug, []stage.Candidate) ([]float64, error) {
				return nil, errors.New("down")
			})}

			got, err := s.Apply(tt.ctx, fakeFile{}, stage.NoDebug, candidates("a", "b"))

			var skipped *stage.Skipped
			if errors.As(err, &skipped) != tt.skipped || err == nil {
				t.Fatalf("err = %v, want an error that is skipped: %v", err, tt.skipped)
			}

			if tt.skipped && ids(got) != "a,b" {
				t.Errorf("got %s, want every candidate unchanged", ids(got))
			}

			if !tt.skipped && got != nil {
				t.Errorf("got %s, want no candidates", ids(got))
			}
		})
	}
}

func TestStage_TiesAtTheTopKCutKeepTheIncomingOrder(t *testing.T) {
	s := stage.Stage{Scorer: fixedScores(0.5, 0.8, 0.5, 0.5), Min: stage.FixedMin(0.3), MaxKeep: 3}

	got, err := s.Apply(t.Context(), fakeFile{}, stage.NoDebug, candidates("a", "b", "c", "d"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ids(got) != "b,a,c" {
		t.Errorf("kept %s, want b,a,c: the tied a, c and d keep the order they arrived in", ids(got))
	}
}
