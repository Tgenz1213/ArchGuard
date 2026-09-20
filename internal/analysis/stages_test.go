package analysis_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

func ptr[T any](v T) *T { return &v }

func buildStages(t *testing.T, cfg *config.Config) ([]stage.Stage, string) {
	t.Helper()
	var warnings bytes.Buffer
	stages := analysis.BuildStages(cfg, index.NewLocalStore(1), &llm.MockProvider{}, &warnings)
	return stages, warnings.String()
}

func cosineSettings(t *testing.T, st stage.Stage) (threshold float64, topK int) {
	t.Helper()
	ranker, ok := st.Scorer.(*stage.CosineRanker)
	if !ok {
		t.Fatalf("scorer = %T, want *stage.CosineRanker", st.Scorer)
	}
	minimum, ok := st.Min.(stage.ADRThreshold)
	if !ok || minimum.Global != ranker.Threshold {
		t.Fatalf("Min = %#v, want ADRThreshold matching the ranker threshold %v", st.Min, ranker.Threshold)
	}
	return ranker.Threshold, st.MaxKeep
}

func configWith(pipeline *config.Pipeline) *config.Config {
	return &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.42},
		Analysis:    config.Analysis{MaxRelevantADRs: 5, Pipeline: pipeline},
	}
}

func TestBuildStages_NoPipelineLeavesDefaults(t *testing.T) {
	stages, warnings := buildStages(t, configWith(nil))
	if stages != nil {
		t.Errorf("stages = %v, want nil", stages)
	}
	if warnings != "" {
		t.Errorf("warnings = %q, want none", warnings)
	}
}

func TestBuildStages_RankFallsBackToExistingSettings(t *testing.T) {
	stages, warnings := buildStages(t, configWith(&config.Pipeline{Rank: &config.StageConfig{Scorer: config.ScorerCosine}}))
	if len(stages) != 1 {
		t.Fatalf("got %d stages, want 1", len(stages))
	}
	threshold, topK := cosineSettings(t, stages[0])
	if threshold != 0.42 || topK != 5 {
		t.Errorf("rank = (%v, %d), want (0.42, 5)", threshold, topK)
	}
	if warnings != "" {
		t.Errorf("warnings = %q, want none", warnings)
	}
}

func TestBuildStages_RankTopKFallsBackToDefaultOfThree(t *testing.T) {
	cfg := configWith(&config.Pipeline{Rank: &config.StageConfig{Scorer: config.ScorerCosine}})
	cfg.Analysis.MaxRelevantADRs = 0
	stages, _ := buildStages(t, cfg)
	if _, topK := cosineSettings(t, stages[0]); topK != 3 {
		t.Errorf("topK = %d, want 3", topK)
	}
}

func TestBuildStages_RankExplicitValuesWin(t *testing.T) {
	stages, _ := buildStages(t, configWith(&config.Pipeline{Rank: &config.StageConfig{Scorer: config.ScorerCosine, Threshold: ptr(0.7), TopK: ptr(9)}}))
	threshold, topK := cosineSettings(t, stages[0])
	if threshold != 0.7 || topK != 9 {
		t.Errorf("rank = (%v, %d), want (0.7, 9)", threshold, topK)
	}
}

func TestBuildStages_RerankAloneRunsAfterDefaultRank(t *testing.T) {
	stages, warnings := buildStages(t, configWith(&config.Pipeline{Rerank: &config.StageConfig{Scorer: config.ScorerCosine}}))
	if len(stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(stages))
	}
	if threshold, topK := cosineSettings(t, stages[0]); threshold != 0.42 || topK != 5 {
		t.Errorf("default rank = (%v, %d), want (0.42, 5)", threshold, topK)
	}
	if threshold, topK := cosineSettings(t, stages[1]); threshold != 0 || topK != 3 {
		t.Errorf("rerank = (%v, %d), want (0, 3)", threshold, topK)
	}
	for _, want := range []string{
		"Warning: analysis.pipeline.rerank.threshold not set, defaulting to 0",
		"Warning: analysis.pipeline.rerank.top_k not set, defaulting to 3",
	} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings %q missing %q", warnings, want)
		}
	}
}

func TestBuildStages_RerankOnlyWarnsForUnsetKeys(t *testing.T) {
	stages, warnings := buildStages(t, configWith(&config.Pipeline{Rerank: &config.StageConfig{Scorer: config.ScorerCosine, Threshold: ptr(0.9)}}))
	if threshold, topK := cosineSettings(t, stages[1]); threshold != 0.9 || topK != 3 {
		t.Errorf("rerank = (%v, %d), want (0.9, 3)", threshold, topK)
	}
	if strings.Contains(warnings, "threshold") || !strings.Contains(warnings, "top_k not set") {
		t.Errorf("warnings = %q, want only the top_k warning", warnings)
	}
}

func TestBuildStages_RankThenRerankInOrder(t *testing.T) {
	stages, warnings := buildStages(t, configWith(&config.Pipeline{
		Rank:   &config.StageConfig{Scorer: config.ScorerCosine, Threshold: ptr(0.3), TopK: ptr(8)},
		Rerank: &config.StageConfig{Scorer: config.ScorerCosine, Threshold: ptr(0.6), TopK: ptr(2)},
	}))
	if len(stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(stages))
	}
	if threshold, topK := cosineSettings(t, stages[0]); threshold != 0.3 || topK != 8 {
		t.Errorf("rank = (%v, %d), want (0.3, 8)", threshold, topK)
	}
	if threshold, topK := cosineSettings(t, stages[1]); threshold != 0.6 || topK != 2 {
		t.Errorf("rerank = (%v, %d), want (0.6, 2)", threshold, topK)
	}
	if warnings != "" {
		t.Errorf("warnings = %q, want none", warnings)
	}
}

func TestBuildStages_OnErrorSetsNameAndPolicy(t *testing.T) {
	skip := &config.StageConfig{OnError: config.OnErrorSkip}
	fail := &config.StageConfig{OnError: config.OnErrorFail}
	tests := []struct {
		name     string
		rank     *config.StageConfig
		rerank   *config.StageConfig
		wantFail []bool
	}{
		{"unset", &config.StageConfig{}, &config.StageConfig{}, []bool{false, false}},
		{"skip", skip, skip, []bool{false, false}},
		{"fail", fail, fail, []bool{true, true}},
		{"rerank alone", nil, fail, []bool{false, true}},
	}
	wantNames := []string{"rank", "rerank"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stages, _ := buildStages(t, configWith(&config.Pipeline{Rank: tt.rank, Rerank: tt.rerank}))
			if len(stages) != 2 {
				t.Fatalf("got %d stages, want 2", len(stages))
			}
			for i, st := range stages {
				if st.Name != wantNames[i] || st.FailOnError != tt.wantFail[i] {
					t.Errorf("stage %d = {Name %q, FailOnError %v}, want {%q, %v}", i, st.Name, st.FailOnError, wantNames[i], tt.wantFail[i])
				}
			}
		})
	}
}

func TestBuildStages_ADROverrideBeatsStageThreshold(t *testing.T) {
	stages, _ := buildStages(t, configWith(&config.Pipeline{Rank: &config.StageConfig{Scorer: config.ScorerCosine, Threshold: ptr(0.8)}}))

	own := &index.ADR{SimilarityThreshold: ptr(0.1)}
	if got := stages[0].Min.For(own); got != 0.1 {
		t.Errorf("threshold for ADR with override = %v, want 0.1", got)
	}
	if got := stages[0].Min.For(&index.ADR{}); got != 0.8 {
		t.Errorf("threshold for ADR without override = %v, want 0.8", got)
	}
}
