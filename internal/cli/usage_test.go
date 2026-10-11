package cli

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

func tallyOf(chatUsage, embedUsage inference.Usage) *telemetry.Tally {
	tally := &telemetry.Tally{}
	tally.RequestSent(telemetry.Judgment, chatUsage, nil)
	tally.RequestSent(telemetry.Embedding, embedUsage, nil)

	return tally
}

func TestUsageReport_Report(t *testing.T) {
	reported := inference.Usage{InputTokens: 2000, OutputTokens: 500, Reported: true}
	priced := &config.Config{
		LLM:         config.LLMConfig{CostPer1KInputTokens: new(0.003), CostPer1KOutputTokens: new(0.015)},
		VectorStore: config.VectorStore{CostPer1KTokens: new(0.0001)},
	}

	tests := []struct {
		name          string
		chat          inference.Usage
		cfg           *config.Config
		wantTokens    bool
		wantChatCost  float64
		wantEmbedCost float64
	}{
		{name: "unreported has no figures even with prices", chat: inference.Usage{}, cfg: priced},
		{name: "reported without prices has tokens only", chat: reported, cfg: &config.Config{}, wantTokens: true},
		{name: "reported with prices has a cost", chat: reported, cfg: priced, wantTokens: true, wantChatCost: 0.0135, wantEmbedCost: 0.0002},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := usageReport(tt.cfg, false, tallyOf(tt.chat, tt.chat))

			assertChatUsage(t, usage.Judgment, tt.wantTokens, tt.chat)
			assertCost(t, "judgment", usage.Judgment.Cost, tt.wantChatCost)
			assertCost(t, "embedding", usage.Embedding.Cost, tt.wantEmbedCost)

			if (usage.Embedding.Tokens != nil) != tt.wantTokens {
				t.Errorf("embedding tokens = %v, want present %v", usage.Embedding.Tokens, tt.wantTokens)
			}
		})
	}
}

func assertChatUsage(t *testing.T, got output.ChatUsage, wantTokens bool, want inference.Usage) {
	t.Helper()

	if !wantTokens {
		if got.InputTokens != nil || got.OutputTokens != nil {
			t.Errorf("tokens = %v/%v, want unavailable", got.InputTokens, got.OutputTokens)
		}

		return
	}

	if got.InputTokens == nil || *got.InputTokens != want.InputTokens || got.OutputTokens == nil || *got.OutputTokens != want.OutputTokens {
		t.Errorf("tokens = %v/%v, want %d/%d", got.InputTokens, got.OutputTokens, want.InputTokens, want.OutputTokens)
	}
}

func assertCost(t *testing.T, role string, got *float64, want float64) {
	t.Helper()

	if want == 0 {
		if got != nil {
			t.Errorf("%s cost = %v, want none", role, *got)
		}

		return
	}

	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s cost = %v, want %v", role, got, want)
	}
}

func TestUsageReport_Counts(t *testing.T) {
	tally := tallyOf(inference.Usage{Reported: true}, inference.Usage{Reported: true})
	tally.RequestSent(telemetry.Suggestion, inference.Usage{Reported: true}, nil)
	tally.CacheChecked(telemetry.Judgment, true)
	tally.CacheChecked(telemetry.Judgment, false)
	tally.RetriesSent(telemetry.Judgment, 3)
	tally.CacheChecked(telemetry.Suggestion, false)
	tally.FileChecked("fast.go", 10*time.Millisecond)
	tally.FileChecked("slow.go", 900*time.Millisecond)

	usage := usageReport(&config.Config{}, true, tally)

	wantJudgment := output.ChatCounts{CacheHits: 1, CacheMisses: 1, Retries: 3}
	if usage.Judgment.Calls != 1 || usage.Judgment.ChatCounts != wantJudgment {
		t.Errorf("judgment = %d call(s), %+v, want 1 and %+v", usage.Judgment.Calls, usage.Judgment.ChatCounts, wantJudgment)
	}

	wantSuggestion := output.ChatCounts{CacheMisses: 1}
	if usage.Suggestion.Calls != 1 || usage.Suggestion.ChatCounts != wantSuggestion {
		t.Errorf("suggestion = %d call(s), %+v, want 1 and %+v", usage.Suggestion.Calls, usage.Suggestion.ChatCounts, wantSuggestion)
	}

	if usage.Embedding.Calls != 1 {
		t.Errorf("embedding calls = %d, want 1", usage.Embedding.Calls)
	}

	wantFiles := []output.FileDuration{{File: "slow.go", DurationMS: 900}, {File: "fast.go", DurationMS: 10}}
	if !slices.Equal(usage.Files, wantFiles) {
		t.Errorf("files = %+v, want %+v", usage.Files, wantFiles)
	}
}

func TestUsageReport_ListsNoFilesAsEmpty(t *testing.T) {
	usage := usageReport(&config.Config{}, false, &telemetry.Tally{})

	if usage.Files == nil {
		t.Error("files is nil, want an empty list so the JSON has [] and not null")
	}
}

func TestNewRecorder_SendsEventsToTheInjectedRecorderToo(t *testing.T) {
	tally, injected := &telemetry.Tally{}, &telemetry.Tally{}

	newRecorder(tally, injected).CacheChecked(telemetry.Judgment, true)

	if tally.Totals(telemetry.Judgment).CacheHits != 1 || injected.Totals(telemetry.Judgment).CacheHits != 1 {
		t.Error("an event reached only one of the run's tally and the injected recorder")
	}
}
