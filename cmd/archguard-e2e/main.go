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

// Marker text is printed when a mock's method is actually invoked, not when
// the mock is constructed -- so e2e tests can prove a call was routed to
// the right provider, not just that both provider objects were built.
const (
	chatMarker  = "Using Mock Chat LLM Provider (E2E)"
	embedMarker = "Using Mock Embed LLM Provider (E2E)"
)

func main() {
	chatProviderFactory := func(cfg *config.Config) llm.Provider {
		mock := &llm.MockProvider{EmbeddingDim: cfg.VectorStore.EmbeddingDim}

		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			fmt.Println(chatMarker)
			if codeContextContainsTrigger(user, testutil.MockViolationTrigger) {
				return `{"violation": true, "reasoning": "Mock violation: trigger found", "quoted_code": "` + testutil.MockViolationTrigger + `"}`, nil
			}
			return `{"violation": false, "reasoning": "Mock: no violation", "quoted_code": ""}`, nil
		}

		// Single-provider configs reuse this instance as embedProvider too, so it must stay functional here.
		mock.EmbedFunc = func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			fmt.Println(chatMarker)
			return defaultMockEmbedding(cfg.VectorStore.EmbeddingDim), nil
		}

		return mock
	}

	// ChatFunc always errors: this provider's Chat method has no legitimate caller, so a call here means a wiring regression.
	embedProviderFactory := func(cfg *config.Config) llm.Provider {
		mock := &llm.MockProvider{EmbeddingDim: cfg.VectorStore.EmbeddingDim}

		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			return "", fmt.Errorf("mock embed-only provider does not support chat")
		}
		mock.EmbedFunc = func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			fmt.Println(embedMarker)
			return defaultMockEmbedding(cfg.VectorStore.EmbeddingDim), nil
		}

		return mock
	}

	factories := cli.ProviderFactories{Chat: chatProviderFactory, Embed: embedProviderFactory}
	if exitCode, err := cli.Execute(factories); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(int(exitCode))
	}
	os.Exit(int(cli.ExitSuccess))
}

// defaultMockEmbedding replicates llm.MockProvider's own zero-value CreateEmbedding fallback (non-zero vector, avoids NaN in cosine similarity).
func defaultMockEmbedding(dim int) []float32 {
	if dim == 0 {
		dim = 1536
	}
	v := make([]float32, dim)
	v[0] = 1.0
	return v
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
