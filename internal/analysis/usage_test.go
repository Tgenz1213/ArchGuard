package analysis_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type meteredRun struct {
	engine     *analysis.Engine
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
	run.engine = engine

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

func TestRun_CountsCacheMissesAndCalls(t *testing.T) {
	run := runMetered(t, nil)

	miss := output.ChatCounts{CacheMisses: 1}
	if got := run.engine.JudgmentCounts; got != miss || run.judgment.Calls() != 1 {
		t.Errorf("judgment = %+v and %d call(s), want %+v and 1", got, run.judgment.Calls(), miss)
	}

	if got := run.engine.SuggestionCounts; got != miss || run.suggestion.Calls() != 1 {
		t.Errorf("suggestion = %+v and %d call(s), want %+v and 1", got, run.suggestion.Calls(), miss)
	}
}

func TestRun_CacheHitCountsOnlyAsAHit(t *testing.T) {
	analysisCache, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	runMetered(t, analysisCache)
	second := runMetered(t, analysisCache)

	hit := output.ChatCounts{CacheHits: 1}
	if got := second.engine.JudgmentCounts; got != hit || second.judgment.Calls() != 0 {
		t.Errorf("judgment = %+v and %d call(s), want %+v and none", got, second.judgment.Calls(), hit)
	}

	if got := second.engine.SuggestionCounts; got != hit || second.suggestion.Calls() != 0 {
		t.Errorf("suggestion = %+v and %d call(s), want %+v and none", got, second.suggestion.Calls(), hit)
	}
}

func TestRun_FailedJudgmentCountsOneMissAndItsCall(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")
	meter := inference.NewMeteredChatter(&inference.MockProvider{ChatFunc: func(context.Context, string, string) (string, error) {
		return "", backoff.Permanent(errors.New("bad request"))
	}})
	h.engine.Chat = meter

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := output.ChatCounts{CacheMisses: 1}
	if got := h.engine.JudgmentCounts; got != want || meter.Calls() != 1 {
		t.Errorf("judgment = %+v and %d call(s), want %+v and 1", got, meter.Calls(), want)
	}
}

func TestRun_ReportsEachFileSlowestFirst(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(_ context.Context, _, user string) (string, error) {
			if strings.Contains(user, "slow.go") {
				time.Sleep(50 * time.Millisecond)
			}

			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.", Embedding: unitVector()}}
	cfg := &config.Config{Analysis: config.Analysis{ExcludePatterns: []string{}}}
	content := &MockContentProvider{Files: map[string]string{"fast.go": "package fast\n", "slow.go": "package slow\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Out = output.Discard()

	runEngine(t, engine, false)

	durations := engine.FileDurations
	if len(durations) != 2 || durations[0].File != "slow.go" || durations[1].File != "fast.go" {
		t.Fatalf("FileDurations = %+v, want slow.go then fast.go", durations)
	}

	if durations[0].DurationMS < 50 {
		t.Errorf("slow.go took %dms, want at least 50", durations[0].DurationMS)
	}
}
