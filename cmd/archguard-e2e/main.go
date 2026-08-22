package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func main() {
	// Markers print on invocation, not construction, so tests can prove a
	// call was routed to the right provider.
	chatProviderFactory := func(cfg *config.Config) llm.Provider {
		mock := &llm.MockProvider{EmbeddingDim: cfg.VectorStore.EmbeddingDim}

		mock.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
			fmt.Println(testutil.MockChatProviderMarker)
			result := llm.AnalysisResult{Violation: false, Reasoning: "Mock: no violation", QuotedCode: ""}
			if codeContextContainsTrigger(user, testutil.MockViolationTrigger) {
				result = llm.AnalysisResult{
					Violation:  true,
					Reasoning:  "Mock violation: trigger found",
					QuotedCode: extractTriggerLine(user, testutil.MockViolationTrigger),
				}
			}
			resp, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			return string(resp), nil
		}

		// Single-provider configs reuse this instance as embedProvider too, so it must stay functional here.
		mock.EmbedFunc = func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			fmt.Println(testutil.MockChatProviderMarker)
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
			fmt.Println(testutil.MockEmbedProviderMarker)
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

// extractTriggerLine returns the trimmed line containing trigger, so
// quoted_code is a real snippet, not the bare trigger word.
func extractTriggerLine(prompt, trigger string) string {
	start := strings.Index(prompt, "<code_context>")
	if start == -1 {
		return ""
	}
	start += len("<code_context>")

	endRelativeOffset := strings.Index(prompt[start:], "</code_context>")
	if endRelativeOffset == -1 {
		return ""
	}

	block := prompt[start : start+endRelativeOffset]
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, trigger) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
