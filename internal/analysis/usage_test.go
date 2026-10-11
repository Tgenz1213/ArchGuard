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
	"github.com/tgenz1213/archguard/internal/telemetry"
)

type meteredRun struct {
	engine *analysis.Engine
	tally  *telemetry.Tally
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

	tally := &telemetry.Tally{}

	engine := analysis.NewEngine(cfg, store, telemetry.NewChatter(provider, telemetry.Judgment, tally), provider, content)
	engine.Cache = analysisCache
	engine.SuggestFixes = true
	engine.SuggestChat = telemetry.NewChatter(provider, telemetry.Suggestion, tally)
	engine.Recorder = tally
	engine.Out = output.Discard()

	runEngine(t, engine, true)

	return meteredRun{engine: engine, tally: tally}
}

func unitVector() []float32 {
	v := make([]float32, 1536)
	v[0] = 1.0

	return v
}

func TestRun_CountsJudgmentAndSuggestionUsageApart(t *testing.T) {
	run := runMetered(t, nil)

	want := telemetry.Totals{Calls: 1, CacheMisses: 1, Usage: inference.Usage{InputTokens: 100, OutputTokens: 10, Reported: true}}
	if got := run.tally.Totals(telemetry.Judgment); got != want {
		t.Errorf("judgment = %+v, want %+v", got, want)
	}

	if got := run.tally.Totals(telemetry.Suggestion); got != want {
		t.Errorf("suggestion = %+v, want %+v", got, want)
	}
}

func TestRun_CacheHitCountsOnlyAsAHit(t *testing.T) {
	analysisCache, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	runMetered(t, analysisCache)
	second := runMetered(t, analysisCache)

	want := telemetry.Totals{CacheHits: 1, Usage: inference.Usage{Reported: true}}
	if got := second.tally.Totals(telemetry.Judgment); got != want {
		t.Errorf("judgment on a cache hit = %+v, want %+v", got, want)
	}

	if got := second.tally.Totals(telemetry.Suggestion); got != want {
		t.Errorf("suggestion on a cache hit = %+v, want %+v", got, want)
	}
}

func TestRun_FailedJudgmentCountsOneMissAndItsCall(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")
	tally := &telemetry.Tally{}
	h.engine.Chat = telemetry.NewChatter(&inference.MockProvider{ChatFunc: func(context.Context, string, string) (string, error) {
		return "", backoff.Permanent(errors.New("bad request"))
	}}, telemetry.Judgment, tally)
	h.engine.Recorder = tally

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := telemetry.Totals{Calls: 1, CacheMisses: 1, Usage: inference.Usage{Reported: true}}
	if got := tally.Totals(telemetry.Judgment); got != want {
		t.Errorf("judgment = %+v, want %+v", got, want)
	}
}

func TestRun_RecordsEachFileItChecked(t *testing.T) {
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
	tally := &telemetry.Tally{}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Recorder = tally
	engine.Out = output.Discard()

	runEngine(t, engine, false)

	files := tally.Files()
	if len(files) != 2 || files[0].File != "slow.go" || files[1].File != "fast.go" {
		t.Fatalf("Files() = %+v, want slow.go then fast.go", files)
	}

	if files[0].Spent < 50*time.Millisecond {
		t.Errorf("slow.go took %v, want at least 50ms", files[0].Spent)
	}
}

func TestRun_WithoutARecorderStillRuns(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.engine.Coverage.ADRChecks != 1 {
		t.Errorf("coverage = %+v, want one ADR check", h.engine.Coverage)
	}
}
