package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

func TestReport_CITruncatedFileIsListedAsSkipped(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) {
			chatCalls++

			return `{"violation": true, "reasoning": "r", "quoted_code": "import python_library"}`, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{
		ID:        "0001",
		Title:     "Use Golang",
		Status:    "Accepted",
		Content:   "All services must be Go.",
		Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
	}}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
		LLM:         config.LLMConfig{MaxTokens: 5},
	}
	bigContent := strings.Repeat("x", 200) + "\nimport python_library\n"
	content := &fallbackOnlyContentProvider{files: map[string]string{"service.py": bigContent}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.CI = true
	engine.Cache = nil

	captureStderr(t, func() { runEngine(t, engine, false) })

	if chatCalls != 0 {
		t.Errorf("a file too large to analyze in CI mode reached the LLM %d time(s)", chatCalls)
	}

	if skipped := engine.Report().SkippedFiles; len(skipped) != 1 || skipped[0].File != "service.py" {
		t.Errorf("expected service.py listed as skipped, got %+v", skipped)
	}
}
