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

// TestResolveEmbedProvider_DifferentProviderNeverFallsBackToChatKey asserts
// an unset embed API key never falls back to the chat provider's key.
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

func TestResolveContentProvider(t *testing.T) {
	tests := []struct {
		name           string
		files          []string
		staged         bool
		all            bool
		updateBaseline bool
		want           analysis.ContentProvider
	}{
		{
			name: "no args or flags defaults to uncommitted",
			want: &analysis.UncommittedProvider{},
		},
		{
			name:  "dot positional arg scans everything",
			files: []string{"."},
			want:  &analysis.AllProvider{},
		},
		{
			name:  "specific file arg scans just that file",
			files: []string{"internal/foo.go"},
			want:  &analysis.SingleFileProvider{Path: "internal/foo.go"},
		},
		{
			name:   "staged flag scans staged files",
			staged: true,
			want:   &analysis.StagedProvider{},
		},
		{
			name: "all flag scans all tracked files",
			all:  true,
			want: &analysis.AllProvider{},
		},
		{
			name:           "update-baseline alone scans everything",
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:           "update-baseline overrides staged",
			staged:         true,
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:           "update-baseline overrides a file arg",
			files:          []string{"internal/foo.go"},
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveContentProvider(tt.files, tt.staged, tt.all, tt.updateBaseline)
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tt.want) {
				t.Fatalf("expected type %T, got %T", tt.want, got)
			}
			if sfp, ok := got.(*analysis.SingleFileProvider); ok {
				wantSFP := tt.want.(*analysis.SingleFileProvider)
				if sfp.Path != wantSFP.Path {
					t.Errorf("expected path %q, got %q", wantSFP.Path, sfp.Path)
				}
			}
		})
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
