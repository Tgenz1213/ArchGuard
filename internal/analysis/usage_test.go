package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type meteredRun struct {
	judgment   *inference.MeteredChatter
	suggestion *inference.MeteredChatter
}

func runMetered(t *testing.T, analysisCache *cache.Cache) meteredRun {
	t.Helper()

	provider := &inference.MockProvider{
		ChatUsage: inference.Usage{InputTokens: 100, OutputTokens: 10, Reported: true},
		ChatFunc: func(_ context.Context, system, _ string) (string, error) {
			if strings.Contains(system, "Remediation Advisor") {
				return `{"suggestion": "Rewrite this in Go."}`, nil
			}

			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.", Embedding: unitVector()}}
	cfg := &config.Config{Analysis: config.Analysis{ExcludePatterns: []string{}}}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	run := meteredRun{judgment: inference.NewMeteredChatter(provider), suggestion: inference.NewMeteredChatter(provider)}

	engine := analysis.NewEngine(cfg, store, run.judgment, provider, content)
	engine.Cache = analysisCache
	engine.SuggestFixes = true
	engine.SuggestChat = run.suggestion
	engine.Out = output.Discard()

	runEngine(t, engine, true)

	return run
}

func unitVector() []float32 {
	v := make([]float32, 1536)
	v[0] = 1.0

	return v
}

func TestRun_CountsJudgmentAndSuggestionUsageApart(t *testing.T) {
	run := runMetered(t, nil)

	want := inference.Usage{InputTokens: 100, OutputTokens: 10, Reported: true}
	if got := run.judgment.Usage(); got != want {
		t.Errorf("judgment usage = %+v, want %+v", got, want)
	}

	if got := run.suggestion.Usage(); got != want {
		t.Errorf("suggestion usage = %+v, want %+v", got, want)
	}
}

func TestRun_CacheHitsAddNoUsage(t *testing.T) {
	analysisCache, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	runMetered(t, analysisCache)
	second := runMetered(t, analysisCache)

	want := inference.Usage{Reported: true}
	if got := second.judgment.Usage(); got != want {
		t.Errorf("judgment usage on a cache hit = %+v, want %+v", got, want)
	}

	if got := second.suggestion.Usage(); got != want {
		t.Errorf("suggestion usage on a cache hit = %+v, want %+v", got, want)
	}
}
