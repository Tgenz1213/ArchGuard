package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
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
