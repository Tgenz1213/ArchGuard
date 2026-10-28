package analysis_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func countingQuestionChat(calls *atomic.Int32) *inference.MockProvider {
	return &inference.MockProvider{ChatFunc: func(ctx context.Context, system, user string) (string, error) {
		calls.Add(1)
		return `{"questions": ["Does this code print a secret?", "  "]}`, nil
	}}
}

func TestQuestionGenerator_GeneratesOncePerADRAcrossFiles(t *testing.T) {
	var calls atomic.Int32
	generator := analysis.NewQuestionGenerator(countingQuestionChat(&calls), nil, "llama3.2", output.Discard())
	adr := &index.ADR{ID: "0001", Content: "No secrets in logs."}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			questions, err := generator.Questions(t.Context(), adr)
			if err != nil || strings.Join(questions, "|") != "Does this code print a secret?" {
				t.Errorf("Questions = %q, %v; want the one non-blank question", questions, err)
			}
		})
	}

	wg.Wait()

	if calls.Load() != 1 {
		t.Errorf("chat calls = %d, want 1", calls.Load())
	}
}

func TestQuestionGenerator_ReusesCachedQuestionsUntilTheADROrModelChanges(t *testing.T) {
	questionCache, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	chat := countingQuestionChat(&calls)
	adr := &index.ADR{ID: "0001", Content: "No secrets in logs."}
	edited := &index.ADR{ID: "0001", Content: "No secrets in logs or metrics."}

	runs := []struct {
		model string
		adr   *index.ADR
		calls int32
	}{
		{"llama3.2", adr, 1},
		{"llama3.2", adr, 1},
		{"llama3.2", edited, 2},
		{"qwen3", edited, 3},
	}

	for i, run := range runs {
		generator := analysis.NewQuestionGenerator(chat, questionCache, run.model, output.Discard())
		if _, err := generator.Questions(t.Context(), run.adr); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}

		if calls.Load() != run.calls {
			t.Errorf("after run %d, chat calls = %d, want %d", i, calls.Load(), run.calls)
		}
	}
}
