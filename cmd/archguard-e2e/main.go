package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
)

func main() {
	// mockFactory provides a deterministic LLM provider for end-to-end testing environments.
	mockFactory := func(cfg *config.Config) llm.Provider {
		fmt.Println("Using Mock LLM Provider (E2E)")

		mock := &llm.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
		}

		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(user, "password") {
				return `{"violation": true, "reasoning": "Mock violation: password found", "quoted_code": "password"}`, nil
			}
			return `{"violation": false, "reasoning": "Mock: no violation", "quoted_code": ""}`, nil
		}

		return mock
	}

	if err := cli.Execute(mockFactory); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
