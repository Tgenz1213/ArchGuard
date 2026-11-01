package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func main() {
	ctx, stop := cli.NotifyContext(context.Background())

	factories := cli.ProviderFactories{Chat: chatProviderFactory(stop), Embed: embedProviderFactory(stop), Jev: jevFactory}
	exitCode, err := cli.Execute(ctx, "e2e", factories)

	stop()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	os.Exit(int(exitCode))
}

var (
	mockChatUsage  = inference.Usage{InputTokens: testutil.MockChatInputTokens, OutputTokens: testutil.MockChatOutputTokens, Reported: true}
	mockEmbedUsage = inference.Usage{InputTokens: testutil.MockEmbedTokens, Reported: true}
)

func chatProviderFactory(stop context.CancelFunc) func(*config.Config) inference.Provider {
	return func(cfg *config.Config) inference.Provider {
		// Single-provider configs reuse this instance as embedProvider too, so it must stay functional here.
		return &inference.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
			ChatFunc:     mockChat(stop),
			EmbedFunc:    mockEmbed(cfg, stop, testutil.MockChatProviderMarker),
			ChatUsage:    mockChatUsage,
			EmbedUsage:   mockEmbedUsage,
		}
	}
}

func embedProviderFactory(stop context.CancelFunc) func(*config.Config) inference.Embedder {
	return func(cfg *config.Config) inference.Embedder {
		return &inference.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
			EmbedFunc:    mockEmbed(cfg, stop, testutil.MockEmbedProviderMarker),
			EmbedUsage:   mockEmbedUsage,
		}
	}
}

type mockJev struct{}

func jevFactory(string) stage.Screener { return mockJev{} }

func (mockJev) Screen(_ context.Context, state string, questions map[string]string) (map[string]float64, error) {
	if strings.Contains(state, testutil.MockJevRejectedKeyTrigger) {
		return nil, &inference.JevError{Status: http.StatusUnauthorized}
	}

	if strings.Contains(state, testutil.MockJevFailureTrigger) {
		return nil, fmt.Errorf("mock jev failure (E2E trigger)")
	}

	answers := make(map[string]float64, len(questions))
	for name, question := range questions {
		answers[name] = 0.05
		if strings.Contains(question, testutil.MockJevBreaksTrigger) {
			answers[name] = 0.95
		}
	}

	return answers, nil
}

// Markers print on invocation, not construction, so tests can prove a
// call was routed to the right provider.
func mockChat(stop context.CancelFunc) func(ctx context.Context, system, user string) (string, error) {
	return func(ctx context.Context, system, user string) (string, error) {
		fmt.Fprintln(os.Stderr, testutil.MockChatProviderMarker)

		if strings.Contains(system, "Remediation Advisor") {
			return `{"suggestion": "Mock suggestion: move this logic into a Go service."}`, nil
		}

		if codeContextContainsTrigger(user, testutil.MockInterruptTrigger) {
			stop()
			return "", ctx.Err()
		}

		if codeContextContainsTrigger(user, testutil.MockChatFailureTrigger) {
			return "", fmt.Errorf("mock chat failure (E2E trigger)")
		}

		result := inference.AnalysisResult{Violation: false, Reasoning: "Mock: no violation", QuotedCode: ""}

		if quote, violation := mockVerdict(user); violation {
			result = inference.AnalysisResult{Violation: true, Reasoning: "Mock violation: trigger found", QuotedCode: quote}
		}

		resp, err := json.Marshal(result)
		if err != nil {
			return "", err
		}

		return string(resp), nil
	}
}

func mockVerdict(prompt string) (quote string, violation bool) {
	if strings.Contains(prompt, "The code context is a unified diff") {
		return diffVerdict(prompt)
	}

	if codeContextContainsTrigger(prompt, testutil.MockUnverifiedTrigger) {
		return "code the model invented", true
	}

	if codeContextContainsTrigger(prompt, testutil.MockViolationTrigger) {
		return extractTriggerLine(prompt, testutil.MockViolationTrigger), true
	}

	return "", false
}

func mockEmbed(cfg *config.Config, stop context.CancelFunc, marker string) func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
	return func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
		fmt.Fprintln(os.Stderr, marker)

		if task == inference.EmbeddingTaskDocument {
			fmt.Fprintln(os.Stderr, testutil.MockEmbedDocumentMarker)
		}

		if strings.Contains(text, testutil.MockInterruptTrigger) {
			stop()
			return nil, ctx.Err()
		}

		if strings.Contains(text, testutil.MockEmbedFailureTrigger) {
			return nil, fmt.Errorf("mock embed failure (E2E trigger)")
		}

		return defaultMockEmbedding(cfg.VectorStore.EmbeddingDim), nil
	}
}

// Mirrors inference.MockProvider's default embedding: non-zero, so cosine similarity avoids NaN.
func defaultMockEmbedding(dim int) []float32 {
	if dim == 0 {
		dim = 1536
	}

	v := make([]float32, dim)
	v[0] = 1.0
	return v
}

func codeContextBlock(prompt string) (string, bool) {
	_, after, found := strings.Cut(prompt, "<code_context>")
	if !found {
		return "", false
	}

	block, _, found := strings.Cut(after, "</code_context>")

	return block, found
}

func codeContextContainsTrigger(prompt, trigger string) bool {
	block, ok := codeContextBlock(prompt)

	return ok && strings.Contains(block, trigger)
}

// extractTriggerLine returns the trimmed line containing trigger, so
// quoted_code is a real snippet, not the bare trigger word.
func extractTriggerLine(prompt, trigger string) string {
	block, _ := codeContextBlock(prompt)
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, trigger) {
			return strings.TrimSpace(line)
		}
	}

	return ""
}

// Stands in for a diff-aware model: an added trigger or a removed required line is a violation; a trigger in unchanged context is reported too, so the engine has something to drop.
func diffVerdict(prompt string) (quote string, violation bool) {
	block, ok := codeContextBlock(prompt)
	if !ok {
		return "", false
	}

	unchanged := ""

	for _, line := range strings.Split(block, "\n") {
		if line == "" {
			continue
		}

		text := strings.TrimSpace(line[1:])

		switch {
		case line[0] == '+' && strings.Contains(line, testutil.MockViolationTrigger):
			return text, true
		case line[0] == '-' && strings.Contains(line, testutil.MockRequiredMarker):
			return text, true
		case line[0] == ' ' && unchanged == "" && strings.Contains(line, testutil.MockViolationTrigger):
			unchanged = text
		}
	}

	return unchanged, unchanged != ""
}
