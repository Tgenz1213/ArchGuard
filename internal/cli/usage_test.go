package cli

import (
	"math"
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func meteredSetup(t *testing.T, chatUsage, embedUsage inference.Usage) usageMeters {
	t.Helper()

	provider := &inference.MockProvider{EmbeddingDim: 2, ChatUsage: chatUsage, EmbedUsage: embedUsage}
	meters := newUsageMeters(runSetup{chat: provider, embed: provider})

	if _, err := meters.judgment.Chat(t.Context(), "system", "user"); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if _, err := meters.embedding.CreateEmbedding(t.Context(), "text", inference.EmbeddingTaskQuery); err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}

	return meters
}

func TestUsageMeters_Report(t *testing.T) {
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
			usage := meteredSetup(t, tt.chat, tt.chat).report(tt.cfg, false)

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

func TestUsageMeters_ReportCountsSuggestionsOnlyWhenOn(t *testing.T) {
	meters := meteredSetup(t, inference.Usage{Reported: true}, inference.Usage{Reported: true})

	if usage := meters.report(&config.Config{}, false); usage.Suggestion != nil {
		t.Errorf("suggestion usage = %+v without --suggest-fixes, want nil", usage.Suggestion)
	}

	if usage := meters.report(&config.Config{}, true); usage.Suggestion == nil {
		t.Error("suggestion usage is nil with --suggest-fixes")
	}
}

func TestValidateCosts(t *testing.T) {
	tests := []struct {
		name    string
		llm     config.LLMConfig
		vector  config.VectorStore
		wantErr bool
	}{
		{name: "none set"},
		{name: "both chat prices and an embedding price", llm: config.LLMConfig{CostPer1KInputTokens: new(0.003), CostPer1KOutputTokens: new(0.015)}, vector: config.VectorStore{CostPer1KTokens: new(0.0)}},
		{name: "only the input price", llm: config.LLMConfig{CostPer1KInputTokens: new(0.003)}, wantErr: true},
		{name: "only the output price", llm: config.LLMConfig{CostPer1KOutputTokens: new(0.015)}, wantErr: true},
		{name: "negative chat price", llm: config.LLMConfig{CostPer1KInputTokens: new(-1.0), CostPer1KOutputTokens: new(0.015)}, wantErr: true},
		{name: "negative embedding price", vector: config.VectorStore{CostPer1KTokens: new(-0.1)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCosts(&config.Config{LLM: tt.llm, VectorStore: tt.vector})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCosts() = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}
