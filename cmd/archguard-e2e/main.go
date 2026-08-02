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
	// CreateEmbedding stays functional here since single-provider configs reuse this as embedProvider too.
	chatProviderFactory := func(cfg *config.Config) llm.Provider {
		fmt.Println("Using Mock Chat LLM Provider (E2E)")

		mock := &llm.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
		}

		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			if codeContextContainsTrigger(user, testutil.MockViolationTrigger) {
				return `{"violation": true, "reasoning": "Mock violation: trigger found", "quoted_code": "` + testutil.MockViolationTrigger + `"}`, nil
			}
			return `{"violation": false, "reasoning": "Mock: no violation", "quoted_code": ""}`, nil
		}

		return mock
	}

	// ChatFunc always errors: this provider's Chat method has no legitimate caller, so a call here means a wiring regression.
	embedProviderFactory := func(cfg *config.Config) llm.Provider {
		fmt.Println("Using Mock Embed LLM Provider (E2E)")

		mock := &llm.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
		}
		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			return "", fmt.Errorf("mock embed-only provider does not support chat")
		}

		return mock
	}

	if exitCode, err := cli.Execute(chatProviderFactory, embedProviderFactory); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(int(exitCode))
	}
	os.Exit(int(cli.ExitSuccess))
}

func codeContextContainsTrigger(prompt, trigger string) bool {
	start := strings.Index(prompt, "<code_context>")
	if start == -1 {
		return false
	}
	start += len("<code_context>")

	endRelativeOffset := strings.Index(prompt[start:], "</code_context>")
	if endRelativeOffset == -1 {
		return false
	}

	return strings.Contains(prompt[start:start+endRelativeOffset], trigger)
}
