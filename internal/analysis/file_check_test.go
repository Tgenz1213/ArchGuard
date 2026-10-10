package analysis_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

const changeSampleHunks = "@@ -1,5 +1,6 @@\n package a\n-var old = 1\n+var added = 1\n+var alsoAdded = 2\n \n func keep() {}\n"

const changeSamplePreamble = "diff --git a/service.py b/service.py\nindex 111..222 100644\n--- a/service.py\n+++ b/service.py\n"

type changeRunOptions struct {
	whole       string
	diff        string
	quote       string
	judgeChange bool
	cache       *cache.Cache
}

type changeRun struct {
	engine    *analysis.Engine
	chatCalls int
}

func runChange(t *testing.T, opts changeRunOptions) *changeRun {
	t.Helper()

	run := &changeRun{}
	vector := make([]float32, 1536)
	vector[0] = 1.0

	provider := &inference.MockProvider{
		EmbedFunc: func(context.Context, string, inference.EmbeddingTaskType) ([]float32, error) {
			return vector, nil
		},
		ChatFunc: func(context.Context, string, string) (string, error) {
			run.chatCalls++

			return fmt.Sprintf(`{"violation": true, "reasoning": "r", "quoted_code": %q}`, opts.quote), nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.", Embedding: vector}}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, &diffCapableContentProvider{content: opts.whole, diff: opts.diff})
	engine.Cache = opts.cache
	engine.JudgeChange = opts.judgeChange
	run.engine = engine

	var drift *analysis.DriftDetectedError
	if err := engine.Run(t.Context()); err != nil && !errors.As(err, &drift) {
		t.Fatalf("Run failed: %v", err)
	}

	return run
}

func TestChange_BinaryDiffIsSkippedSilently(t *testing.T) {
	run := runChange(t, changeRunOptions{whole: "x", diff: "diff --git a/x b/x\nBinary files a/x and b/x differ\n", quote: "x", judgeChange: true})

	if run.chatCalls != 0 || len(run.engine.SkippedFiles) != 0 || len(run.engine.CollectedViolations) != 0 {
		t.Fatalf("chat calls = %d, skipped = %+v, violations = %+v; want nothing judged, nothing recorded", run.chatCalls, run.engine.SkippedFiles, run.engine.CollectedViolations)
	}
}

func TestChange_Findings(t *testing.T) {
	tests := []struct {
		name         string
		quote        string
		wantFindings int
		wantLine     int
		wantVerified bool
	}{
		{"a quote in unchanged context is dropped", "func keep() {}", 0, 0, false},
		{"a quote in an added line keeps its new-file line", "var added = 1", 1, 2, true},
		{"a quote in a removed line keeps the line it was removed from", "var old = 1", 1, 2, true},
		{"a quote found nowhere stays unverified", "something the model invented", 1, 0, false},
		{"an empty quote is unverified, not verified", "", 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runChange(t, changeRunOptions{whole: "ignored", diff: changeSamplePreamble + changeSampleHunks, quote: tt.quote, judgeChange: true})

			found := run.engine.CollectedViolations
			if len(found) != tt.wantFindings {
				t.Fatalf("findings = %+v, want %d", found, tt.wantFindings)
			}

			if tt.wantFindings == 0 {
				return
			}

			if found[0].Line != tt.wantLine {
				t.Errorf("line = %d, want %d", found[0].Line, tt.wantLine)
			}

			if got := run.engine.Report().Violations[0].Verified; got != tt.wantVerified {
				t.Errorf("verified = %v, want %v", got, tt.wantVerified)
			}
		})
	}
}

func TestChange_CacheKeyDiffersFromWholeFileJudging(t *testing.T) {
	shared, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	runChange(t, changeRunOptions{whole: changeSampleHunks, quote: "var added = 1", cache: shared})
	second := runChange(t, changeRunOptions{whole: "ignored", diff: changeSamplePreamble + changeSampleHunks, quote: "var added = 1", judgeChange: true, cache: shared})

	if second.chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1: a --since run must not be served the whole-file verdict for identical text", second.chatCalls)
	}
}
