package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
)

func TestExitCodeForAnalysisError(t *testing.T) {
	t.Run("returns drift exit code for direct drift detection errors", func(t *testing.T) {
		err := &analysis.DriftDetectedError{Count: 2}
		if got := exitCodeForAnalysisError(err); got != ExitDriftDetected {
			t.Fatalf("expected %d, got %d", ExitDriftDetected, got)
		}
	})

	t.Run("returns drift exit code for wrapped drift detection errors", func(t *testing.T) {
		err := fmt.Errorf("wrapped: %w", &analysis.DriftDetectedError{Count: 2})
		if got := exitCodeForAnalysisError(err); got != ExitDriftDetected {
			t.Fatalf("expected %d, got %d", ExitDriftDetected, got)
		}
	})

	t.Run("returns generic error exit code for operational errors", func(t *testing.T) {
		err := errors.New("git content provider failure")
		if got := exitCodeForAnalysisError(err); got != ExitError {
			t.Fatalf("expected %d, got %d", ExitError, got)
		}
	})
}

func TestValidateProviderConfig_ClaudeRequiresEmbeddingProvider(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: ""},
	}
	if err := validateProviderConfig(cfg); err == nil {
		t.Fatal("expected an error when llm.provider is claude and vector_store.provider is unset")
	}
}

func TestValidateProviderConfig_ClaudeWithEmbeddingProviderOK(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	if err := validateProviderConfig(cfg); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateProviderConfig_NonClaudeProvidersUnaffected(t *testing.T) {
	for _, provider := range []string{"openai", "ollama", "gemini"} {
		cfg := &config.Config{
			LLM:         config.LLMConfig{Provider: provider},
			VectorStore: config.VectorStore{Provider: ""},
		}
		if err := validateProviderConfig(cfg); err != nil {
			t.Errorf("provider %q: expected no error with vector_store.provider unset, got: %v", provider, err)
		}
	}
}

func TestValidateProviderConfig_VoyageRejectedAsLLMProvider(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{Provider: "voyage"},
	}
	if err := validateProviderConfig(cfg); err == nil {
		t.Fatal("expected an error when llm.provider is voyage (embeddings-only, no chat capability)")
	}
}

func TestValidateProviderConfig_ClaudeRejectedAsEmbeddingProvider(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "gemini"},
		VectorStore: config.VectorStore{Provider: "claude"},
	}
	if err := validateProviderConfig(cfg); err == nil {
		t.Fatal("expected an error when vector_store.provider is claude (chat-only, no embeddings capability)")
	}
}

func TestResolveEmbedProvider_SameProviderReusesInstance(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "openai"},
		VectorStore: config.VectorStore{Provider: ""},
	}
	name, _, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if !reuse {
		t.Error("expected reuse=true when vector_store.provider is unset")
	}
	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
}

func TestResolveEmbedProvider_ExplicitSameProviderReusesInstance(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "openai"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	_, _, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if !reuse {
		t.Error("expected reuse=true when vector_store.provider explicitly matches llm.provider")
	}
}

func TestResolveEmbedProvider_DifferentProviderUsesEmbedKey(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	name, apiKey, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if reuse {
		t.Error("expected reuse=false for different providers")
	}
	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
	if apiKey != "embed-key" {
		t.Errorf("expected embed-key, got %q", apiKey)
	}
}

// TestResolveEmbedProvider_DifferentProviderNeverFallsBackToChatKey is the
// regression guard for the credential-leak bug fixed in commit fee5a7c:
// when the embed provider differs from the chat provider and the embed
// API key env var is unset, the chat provider's key must NEVER be used as
// a substitute -- that would send one vendor's credential to a different
// vendor's API.
func TestResolveEmbedProvider_DifferentProviderNeverFallsBackToChatKey(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	name, apiKey, reuse := resolveEmbedProvider(cfg, "chat-key", "")
	if reuse {
		t.Error("expected reuse=false for different providers")
	}
	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
	if apiKey == "chat-key" {
		t.Fatal("REGRESSION: embed provider fell back to the chat provider's API key -- this is the exact credential-leak bug fixed in fee5a7c")
	}
	if apiKey != "" {
		t.Errorf("expected empty apiKey (embed key was unset, must not substitute chat key), got %q", apiKey)
	}
}

func TestResolveEmbedProviderInstance_ReusesChatProviderWhenNamesMatch(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "openai"}}
	chat := &llm.MockProvider{}

	got, err := resolveEmbedProviderInstance(cfg, chat, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != llm.Provider(chat) {
		t.Error("expected the chat provider instance to be reused")
	}
}

func TestResolveEmbedProviderInstance_BuildsFromFactoryWhenNamesDiffer(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	chat := &llm.MockProvider{}
	embed := &llm.MockProvider{}

	got, err := resolveEmbedProviderInstance(cfg, chat, func(*config.Config) llm.Provider { return embed })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != llm.Provider(embed) {
		t.Error("expected the embed factory's provider to be used, not the chat provider")
	}
}

func TestResolveEmbedProviderInstance_ErrorsWhenEmbedFactoryRequiredButNil(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	chat := &llm.MockProvider{}

	if _, err := resolveEmbedProviderInstance(cfg, chat, nil); err == nil {
		t.Fatal("expected an error when the roles need different providers but embedFactory is nil")
	}
}

func TestBuildProvider_ClaudeAndVoyage(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Model: "claude-sonnet-4-5"},
		VectorStore: config.VectorStore{Model: "voyage-4"},
	}

	claude, err := buildProvider("claude", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(claude) failed: %v", err)
	}
	if _, ok := claude.(*llm.ClaudeProvider); !ok {
		t.Errorf("expected *llm.ClaudeProvider, got %T", claude)
	}

	voyage, err := buildProvider("voyage", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(voyage) failed: %v", err)
	}
	if _, ok := voyage.(*llm.VoyageProvider); !ok {
		t.Errorf("expected *llm.VoyageProvider, got %T", voyage)
	}
}
