package analysis_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

type scorerFunc func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error)

func (f scorerFunc) Score(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
	return f(ctx, file, debug, candidates)
}

func scoresByID(scores map[string]float64, seen *[][]string) stage.Scorer {
	return scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		ids := make([]string, len(candidates))
		out := make([]float64, len(candidates))
		for i, c := range candidates {
			ids[i] = c.ADR.ID
			out[i] = scores[c.ADR.ID]
		}
		if seen != nil {
			*seen = append(*seen, ids)
		}
		return out, nil
	})
}

type scorerHarness struct {
	engine *analysis.Engine
	mu     sync.Mutex
	judged []string
	embeds int
}

func newScorerHarness(t *testing.T, adrs []index.ADR, file, fileContent string) *scorerHarness {
	t.Helper()
	h := &scorerHarness{}
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, a := range adrs {
				if strings.Contains(user, a.Content) {
					h.judged = append(h.judged, a.ID)
				}
			}
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			h.mu.Lock()
			h.embeds++
			h.mu.Unlock()
			v := make([]float32, 4)
			v[0] = 1
			return v, nil
		},
	}

	store := index.NewLocalStore(1)
	store.ADRs = adrs

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{MaxRelevantADRs: 3},
	}
	content := &MockContentProvider{Files: map[string]string{file: fileContent}}

	h.engine = analysis.NewEngine(cfg, store, provider, content, false, false)
	h.engine.Cache = nil
	return h
}

func scorerADR(id string, similarity float32) index.ADR {
	emb := make([]float32, 4)
	emb[0] = similarity
	emb[1] = 1 - similarity
	return index.ADR{ID: id, Title: "ADR " + id, Status: "Accepted", RelPath: id + ".md", Content: "rule-body-" + id, Embedding: emb}
}

func TestPipeline_ScoresAllCandidatesInOneCall(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1), scorerADR("0003", 1)}, "svc.go", "package svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 1, "0003": 1}, &calls)}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 || len(calls[0]) != 3 {
		t.Fatalf("scorer calls = %v, want one call with all 3 candidates", calls)
	}
	if len(h.judged) != 3 {
		t.Fatalf("judged %v, want all 3", h.judged)
	}
}

func TestPipeline_OnlyWhatTheStageKeepsIsJudged(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1), scorerADR("0003", 1), scorerADR("0004", 1)}, "svc.go", "package svc")

	h.engine.Stages = []stage.Stage{{
		Scorer:  scoresByID(map[string]float64{"0001": 0.9, "0002": 0.2, "0003": 0.7, "0004": 0.8}, nil),
		Min:     stage.FixedMin(0.5),
		MaxKeep: 2,
	}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(h.judged, ",") != "0001,0004" {
		t.Fatalf("judged %v, want the two best scorers above the minimum, best first (0001,0004)", h.judged)
	}
}

func TestPipeline_StagesRunInOrderOverSurvivors(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1), scorerADR("0003", 1)}, "svc.go", "package svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{
		{Scorer: scoresByID(map[string]float64{"0001": 0.9, "0002": 0.8, "0003": 0.1}, &calls), Min: stage.FixedMin(0.5)},
		{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 0}, &calls), Min: stage.FixedMin(0.5)},
	}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 || len(calls[1]) != 2 {
		t.Fatalf("scorer calls = %v, want the second stage to see only the first stage's 2 survivors", calls)
	}
	if strings.Join(h.judged, ",") != "0001" {
		t.Fatalf("judged %v, want only 0001", h.judged)
	}
}

func TestPipeline_WithoutCosineMakesNoEmbeddingCalls(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "svc.go", "package svc")
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1}, nil)}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.embeds != 0 {
		t.Fatalf("made %d embedding calls with no cosine ranker, want 0", h.embeds)
	}
	if len(h.judged) != 1 {
		t.Fatalf("judged %v, want the one candidate judged", h.judged)
	}
}

func TestPipeline_OnlyScopeMatchedADRsAreCandidates(t *testing.T) {
	inScope := scorerADR("0001", 1)
	inScope.Scope = index.ScopePatterns{"**/*.go"}
	outOfScope := scorerADR("0002", 1)
	outOfScope.Scope = index.ScopePatterns{"**/*.py"}
	h := newScorerHarness(t, []index.ADR{inScope, outOfScope}, "svc.go", "package svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 1}, &calls)}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != "0001" {
		t.Fatalf("scorer calls = %v, want only the in-scope ADR 0001", calls)
	}
}

func TestPipeline_SuppressedADRsNeverReachScorerOrLLM(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "svc.go", "// archguard-ignore: 0001\npackage svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 1}, &calls)}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != "0002" {
		t.Fatalf("scorer calls = %v, want only the unsuppressed ADR 0002", calls)
	}
	if len(h.judged) != 1 || h.judged[0] != "0002" {
		t.Fatalf("judged %v, want only ADR 0002", h.judged)
	}
}

func TestPipeline_ScorerErrorSkipsFile(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "svc.go", "package svc")
	h.engine.Stages = []stage.Stage{{Scorer: scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		return nil, errors.New("scorer down")
	})}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.engine.SkippedFiles != 1 || len(h.judged) != 0 {
		t.Fatalf("SkippedFiles = %d, judged = %v; want the file skipped with nothing judged", h.engine.SkippedFiles, h.judged)
	}
}

func TestPipeline_DefaultCosineSuppressedADRDoesNotConsumeTopKSlot(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 0.9), scorerADR("0003", 0.8)}, "svc.go", "// archguard-ignore: 0001\npackage svc")
	h.engine.Config.Analysis.MaxRelevantADRs = 2

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(h.judged, ",") != "0002,0003" {
		t.Fatalf("judged %v, want the two highest-ranked unsuppressed ADRs (0002,0003)", h.judged)
	}
}

func TestPipeline_DefaultCosineEmbedsOncePerFile(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 0.9)}, "svc.go", "package svc")

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.embeds != 1 {
		t.Fatalf("made %d embedding calls, want 1 for one file", h.embeds)
	}
	if len(h.judged) != 2 {
		t.Fatalf("judged %v, want both ADRs", h.judged)
	}
}

func TestPipeline_EmptyStagesFallsBackToTheDefaultCosineStage(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 0.9), scorerADR("0003", 0.8)}, "svc.go", "package svc")
	h.engine.Config.Analysis.MaxRelevantADRs = 2
	h.engine.Stages = []stage.Stage{}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.embeds != 1 || len(h.judged) != 2 {
		t.Fatalf("embeds = %d, judged = %v; want the default cosine stage (1 embed, top-2 judged), not every ADR", h.embeds, h.judged)
	}
}

type failingScopedStore struct{ index.VectorStore }

func (failingScopedStore) ScopedADRs(string) ([]index.SearchResult, error) {
	return nil, errors.New("db down")
}

func TestPipeline_CandidateLoadFailureSkipsTheFileInsteadOfPassingIt(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "svc.go", "package svc")
	h.engine.Store = failingScopedStore{VectorStore: h.engine.Store}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.engine.SkippedFiles != 1 || len(h.judged) != 0 || h.embeds != 0 {
		t.Fatalf("SkippedFiles = %d, judged = %v, embeds = %d; want the file reported as skipped with nothing judged", h.engine.SkippedFiles, h.judged, h.embeds)
	}
}
