package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFromYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archguard.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(path)
}

func TestLoadConfig_PipelineAbsent(t *testing.T) {
	cfg, err := loadFromYAML(t, "analysis:\n  adr_path: docs\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Analysis.Pipeline != nil {
		t.Errorf("Pipeline = %+v, want nil", cfg.Analysis.Pipeline)
	}
}

func TestLoadConfig_PipelineValid(t *testing.T) {
	cfg, err := loadFromYAML(t, `analysis:
  pipeline:
    rank:
      scorer: cosine
      threshold: 0.6
      top_k: 5
    rerank:
      top_k: 2
`)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Analysis.Pipeline
	if p == nil || p.Rank == nil || p.Rerank == nil {
		t.Fatalf("Pipeline = %+v, want both stages", p)
	}
	if p.Rank.Scorer != ScorerCosine || p.Rank.Threshold == nil || *p.Rank.Threshold != 0.6 || p.Rank.TopK == nil || *p.Rank.TopK != 5 {
		t.Errorf("Rank = %+v", p.Rank)
	}
	if p.Rerank.Scorer != ScorerCosine {
		t.Errorf("Rerank.Scorer = %q, want default %q", p.Rerank.Scorer, ScorerCosine)
	}
	if p.Rerank.Threshold != nil || p.Rerank.TopK == nil || *p.Rerank.TopK != 2 {
		t.Errorf("Rerank = %+v", p.Rerank)
	}
}

func TestLoadConfig_PipelineRerankAlone(t *testing.T) {
	cfg, err := loadFromYAML(t, "analysis:\n  pipeline:\n    rerank:\n      threshold: 0.5\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Analysis.Pipeline.Rank != nil {
		t.Errorf("Rank = %+v, want nil", cfg.Analysis.Pipeline.Rank)
	}
	if cfg.Analysis.Pipeline.Rerank == nil {
		t.Error("Rerank is nil")
	}
}

func TestLoadConfig_PipelineEmptyStage(t *testing.T) {
	cfg, err := loadFromYAML(t, "analysis:\n  pipeline:\n    rerank:\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Analysis.Pipeline.Rerank == nil || cfg.Analysis.Pipeline.Rerank.Scorer != ScorerCosine {
		t.Errorf("Rerank = %+v, want present cosine stage", cfg.Analysis.Pipeline.Rerank)
	}
}

func TestLoadConfig_PipelineAliasSharesStageSettings(t *testing.T) {
	cfg, err := loadFromYAML(t, `analysis:
  pipeline:
    rank: &shared
      threshold: 0.6
      top_k: 4
    rerank: *shared
`)
	if err != nil {
		t.Fatal(err)
	}
	for name, stage := range map[string]*StageConfig{"rank": cfg.Analysis.Pipeline.Rank, "rerank": cfg.Analysis.Pipeline.Rerank} {
		if stage == nil || stage.Threshold == nil || *stage.Threshold != 0.6 || stage.TopK == nil || *stage.TopK != 4 {
			t.Errorf("%s = %+v, want threshold 0.6 and top_k 4", name, stage)
		}
	}
}

func TestLoadConfig_PipelineMergeKeys(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantThreshold *float64
		wantTopK      int
	}{
		{
			name: "explicit key overrides merged one",
			yaml: `analysis:
  pipeline:
    rank: &base
      threshold: 0.6
      top_k: 4
    rerank:
      <<: *base
      top_k: 2
`,
			wantThreshold: new(0.6),
			wantTopK:      2,
		},
		{
			name: "earlier mapping in a merge sequence wins",
			yaml: `analysis:
  pipeline:
    rank: &first
      top_k: 1
    rerank:
      <<: [*first, {top_k: 9, threshold: 0.5}]
`,
			wantThreshold: new(0.5),
			wantTopK:      1,
		},
		{
			name: "nested merge",
			yaml: `analysis:
  pipeline:
    rank: &base
      threshold: 0.3
    rerank: &mid
      <<: *base
      top_k: 7
`,
			wantThreshold: new(0.3),
			wantTopK:      7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadFromYAML(t, tt.yaml)
			if err != nil {
				t.Fatal(err)
			}
			rerank := cfg.Analysis.Pipeline.Rerank
			if rerank == nil || rerank.Threshold == nil || *rerank.Threshold != *tt.wantThreshold || rerank.TopK == nil || *rerank.TopK != tt.wantTopK {
				t.Errorf("rerank = %+v, want threshold %v and top_k %d", rerank, *tt.wantThreshold, tt.wantTopK)
			}
		})
	}
}

func TestLoadConfig_PipelineOnError(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unset", "rank:\n      top_k: 2", ""},
		{"skip", "rank:\n      on_error: skip", OnErrorSkip},
		{"fail", "rank:\n      on_error: fail", OnErrorFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadFromYAML(t, "analysis:\n  pipeline:\n    "+tt.yaml+"\n")
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Analysis.Pipeline.Rank.OnError; got != tt.want {
				t.Errorf("OnError = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadConfig_PipelineInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{"unknown scorer", "rank:\n      scorer: jev", []string{"analysis.pipeline.rank.scorer", `"jev"`, "cosine"}},
		{"unrecognized stage", "pre_judge:\n      scorer: cosine", []string{"analysis.pipeline", `"pre_judge"`}},
		{"unrecognized stage key", "rerank:\n      retries: 2", []string{"analysis.pipeline.rerank", `"retries"`}},
		{"unknown on_error", "rank:\n      on_error: warn", []string{"analysis.pipeline.rank.on_error", `"warn"`, "skip, fail"}},
		{"non-string on_error", "rerank:\n      on_error: [skip]", []string{"analysis.pipeline.rerank.on_error", "skip, fail"}},
		{"null on_error", "rank:\n      on_error:", []string{"analysis.pipeline.rank.on_error", "skip, fail"}},
		{"non-numeric threshold", "rank:\n      threshold: high", []string{"analysis.pipeline.rank.threshold", "number"}},
		{"threshold above range", "rerank:\n      threshold: 1.5", []string{"analysis.pipeline.rerank.threshold", "1.5", "between 0 and 1"}},
		{"threshold below range", "rank:\n      threshold: -0.1", []string{"analysis.pipeline.rank.threshold", "-0.1"}},
		{"threshold nan", "rank:\n      threshold: .nan", []string{"analysis.pipeline.rank.threshold"}},
		{"zero top_k", "rank:\n      top_k: 0", []string{"analysis.pipeline.rank.top_k", "positive"}},
		{"negative top_k", "rerank:\n      top_k: -2", []string{"analysis.pipeline.rerank.top_k", "positive"}},
		{"non-integer top_k", "rank:\n      top_k: 2.5", []string{"analysis.pipeline.rank.top_k", "integer"}},
		{"non-numeric top_k", "rank:\n      top_k: many", []string{"analysis.pipeline.rank.top_k", "integer"}},
		{"null threshold", "rank:\n      threshold:", []string{"analysis.pipeline.rank.threshold", "number"}},
		{"duplicate stage key", "rank:\n      top_k: 1\n      top_k: 2", []string{"analysis.pipeline.rank", `"top_k" already defined`}},
		{"duplicate stage", "rank:\n      top_k: 1\n    rank:\n      top_k: 2", []string{"analysis.pipeline", `"rank" already defined`}},
		{"merge from a non-mapping", "rerank:\n      <<: 5", []string{"analysis.pipeline.rerank", "map merge requires"}},
		{"stage not a mapping", "rank: cosine", []string{"analysis.pipeline.rank", "mapping"}},
		{"pipeline not a mapping", "[rank]", []string{"analysis.pipeline", "mapping"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFromYAML(t, "analysis:\n  pipeline:\n    "+tt.yaml+"\n")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
