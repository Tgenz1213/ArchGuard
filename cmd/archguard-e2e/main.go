package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func main() {
	// mockFactory provides a deterministic LLM provider for end-to-end testing environments.
	mockFactory := func(cfg *config.Config) llm.Provider {
		fmt.Println("Using Mock LLM Provider (E2E)")

		mock := &llm.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
		}

		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(user, testutil.MockViolationTrigger) {
				return `{"violation": true, "reasoning": "Mock violation: trigger found", "quoted_code": "` + testutil.MockViolationTrigger + `"}`, nil
			}
			return `{"violation": false, "reasoning": "Mock: no violation", "quoted_code": ""}`, nil
		}

		return mock
	}

	if exitCode, err := cli.Execute(mockFactory); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(int(exitCode))
	}
	os.Exit(int(cli.ExitSuccess))
}
