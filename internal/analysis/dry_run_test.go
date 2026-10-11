package analysis_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

const promptTokens = 250

func dryRunEngine(t *testing.T, provider *inference.MockProvider, analysisCache *cache.Cache) *analysis.Engine {
	t.Helper()

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.", Embedding: unitVector()},
		{ID: "0002", Title: "No Python", Status: "Accepted", Content: "No Python imports.", Embedding: unitVector()},
	}
	cfg := &config.Config{Analysis: config.Analysis{ExcludePatterns: []string{}}}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = analysisCache
	engine.DryRun = true
	engine.Out = output.Discard()

	return engine
}

func violatingProvider() *inference.MockProvider {
	return &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) {
			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
}

func countPrompts(countErr error) func(context.Context, string) (int, error) {
	return func(_ context.Context, text string) (int, error) {
		if !strings.HasPrefix(text, inference.DefaultSystemPrompt) {
			return 10, nil
		}

		return promptTokens, countErr
	}
}

func TestDryRun_CountsThePromptsInsteadOfSendingThem(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) {
			t.Error("a dry run sent a chat call")
			return "", nil
		},
		CountTokensFunc: countPrompts(nil),
	}
	engine := dryRunEngine(t, provider, nil)

	runEngine(t, engine, false)

	want := analysis.Projection{Calls: 2, Usage: inference.Usage{InputTokens: 2 * promptTokens, Reported: true}}
	if engine.Projection != want {
		t.Errorf("Projection = %+v, want %+v", engine.Projection, want)
	}

	if got, want := engine.JudgmentCounts, (output.ChatCounts{CacheMisses: 2}); got != want {
		t.Errorf("JudgmentCounts = %+v, want %+v", got, want)
	}

	if len(engine.CollectedViolations) != 0 {
		t.Errorf("a dry run reported %d violations", len(engine.CollectedViolations))
	}
}

func TestDryRun_CachedVerdictCostsNothing(t *testing.T) {
	analysisCache, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	warm := dryRunEngine(t, violatingProvider(), analysisCache)
	warm.DryRun = false
	runEngine(t, warm, true)

	provider := violatingProvider()
	provider.CountTokensFunc = countPrompts(nil)
	engine := dryRunEngine(t, provider, analysisCache)

	runEngine(t, engine, false)

	want := analysis.Projection{Cached: 2, Usage: inference.Usage{Reported: true}}
	if engine.Projection != want {
		t.Errorf("Projection = %+v, want %+v", engine.Projection, want)
	}

	if got, want := engine.JudgmentCounts, (output.ChatCounts{CacheHits: 2}); got != want {
		t.Errorf("JudgmentCounts = %+v, want %+v", got, want)
	}
}

func TestDryRun_FailedCountMakesTheProjectionUnavailable(t *testing.T) {
	provider := &inference.MockProvider{CountTokensFunc: countPrompts(errors.New("tokenizer down"))}
	engine := dryRunEngine(t, provider, nil)

	runEngine(t, engine, false)

	if engine.Projection.Usage.Reported {
		t.Errorf("Projection = %+v, want unreported usage", engine.Projection)
	}

	if len(engine.FailedChecks) != 2 {
		t.Errorf("FailedChecks = %+v, want one per ADR", engine.FailedChecks)
	}
}
