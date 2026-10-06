package analysis_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

func TestLog_CompliantFileIsSilentUnlessDebug(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) {
			return `{"violation": false, "reasoning": "fine", "quoted_code": ""}`, nil
		},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	run := func(debug bool) string {
		store := index.NewLocalStore(5)
		store.ADRs = []index.ADR{{
			ID:        "0001",
			Title:     "Use Golang",
			Status:    "Accepted",
			Content:   "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
		}}
		content := &MockContentProvider{Files: map[string]string{"service.go": "package main\n"}}

		engine := analysis.NewEngine(cfg, store, provider, provider, content)
		engine.Cache = nil
		engine.Debug = debug

		return captureStderr(t, func() { runEngine(t, engine, false) })
	}

	if got := run(false); got != "" {
		t.Errorf("a compliant file logged without --debug: %q", got)
	}

	if got := run(true); got == "" {
		t.Error("--debug logged nothing for a compliant file, so the silent case proves nothing")
	}
}

func TestLog_FileWithNoRelevantADRsIsSilentUnlessDebug(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) {
			t.Error("the LLM was called for a file with no relevant ADRs")

			return "", nil
		},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.go": "package main\n"}}

	engine := analysis.NewEngine(cfg, index.NewLocalStore(5), provider, provider, content)
	engine.Cache = nil

	if got := captureStderr(t, func() { runEngine(t, engine, false) }); got != "" {
		t.Errorf("a file with no relevant ADRs logged without --debug: %q", got)
	}
}

func TestLog_DebugLogsOneOutcomeLinePerADRCheck(t *testing.T) {
	compliant := `{"violation": false, "reasoning": "fine", "quoted_code": ""}`
	violation := `{"violation": true, "reasoning": "bad", "quoted_code": "package main"}`

	runDebug := func(chat inference.Chatter, cacheDir string) string {
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
		}
		content := &MockContentProvider{Files: map[string]string{"service.go": "package main\n"}}

		engine := analysis.NewEngine(cfg, store, chat, &inference.MockProvider{}, content)
		engine.Debug = true
		engine.Cache = nil

		if cacheDir != "" {
			analysisCache, err := cache.NewCache(cacheDir)
			if err != nil {
				t.Fatalf("cache.NewCache failed: %v", err)
			}

			engine.Cache = analysisCache
		}

		return captureStderr(t, func() {
			if err := engine.Run(t.Context()); err != nil && !errors.Is(err, analysis.ErrDriftDetected) {
				t.Errorf("Run() = %v, want nil or drift", err)
			}
		})
	}

	outcomeLines := func(logText string) []string {
		var lines []string

		for _, line := range strings.Split(logText, "\n") {
			if strings.Contains(line, "[DEBUG]") && strings.Contains(line, "ADR 0001") {
				lines = append(lines, line)
			}
		}

		return lines
	}

	chatReturning := func(response string) *inference.MockProvider {
		return &inference.MockProvider{ChatFunc: func(context.Context, string, string) (string, error) { return response, nil }}
	}

	tests := []struct {
		name   string
		run    func() string
		want   []string
		absent []string
	}{
		{"compliant", func() string { return runDebug(chatReturning(compliant), "") }, []string{"compliant"}, []string{"violation", "failed", "cached"}},
		{"violation", func() string { return runDebug(chatReturning(violation), "") }, []string{"violation"}, []string{"compliant", "failed", "cached"}},
		{"failed", func() string {
			failing := &inference.MockProvider{ChatFunc: func(context.Context, string, string) (string, error) {
				return "", backoff.Permanent(errors.New("llm down"))
			}}

			return runDebug(failing, "")
		}, []string{"failed"}, []string{"compliant", "violation", "cached"}},
		{"cached", func() string {
			dir := t.TempDir()
			runDebug(chatReturning(compliant), dir)

			return runDebug(chatReturning(compliant), dir)
		}, []string{"compliant", "cached"}, []string{"violation", "failed"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := outcomeLines(tt.run())
			if len(lines) != 1 {
				t.Fatalf("want exactly one outcome line for the ADR check, got %d: %q", len(lines), lines)
			}

			for _, word := range tt.want {
				if !strings.Contains(lines[0], word) {
					t.Errorf("outcome line %q does not say %q", lines[0], word)
				}
			}

			for _, word := range tt.absent {
				if strings.Contains(lines[0], word) {
					t.Errorf("outcome line %q wrongly says %q", lines[0], word)
				}
			}
		})
	}
}
