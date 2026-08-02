package llm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const claudeBaseURL = "https://api.anthropic.com"

// claudeMaxResponseTokens bounds Claude's generated response. ArchGuard's
// prompts (internal/llm/llm.go's ChatPrompt) always ask for a short JSON
// object, so a fixed budget -- not a config field -- is sufficient.
const claudeMaxResponseTokens = 1024

type ClaudeProvider struct {
	client anthropic.Client
	model  string
}

// NewClaudeProvider constructs a ClaudeProvider that talks to the real
// Anthropic API.
func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
	return NewClaudeProviderWithBaseURL(apiKey, model, claudeBaseURL, &http.Client{})
}

// NewClaudeProviderWithBaseURL constructs a ClaudeProvider pointed at a
// custom base URL using a custom HTTP client. This exists primarily so
// tests can inject an httptest.Server instead of hitting the real
// Anthropic API.
func NewClaudeProviderWithBaseURL(apiKey, model, baseURL string, httpClient *http.Client) *ClaudeProvider {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
	)
	return &ClaudeProvider{client: client, model: model}
}

func (p *ClaudeProvider) Chat(ctx context.Context, system, user string) (string, error) {
	message, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: claudeMaxResponseTokens,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude chat request failed: %w", err)
	}

	for _, block := range message.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			return textBlock.Text, nil
		}
	}
	return "", fmt.Errorf("claude returned no text content")
}

// CreateEmbedding always fails: Anthropic has no embeddings API. Callers
// must configure vector_store.provider to a different, embedding-capable
// provider -- see docs/arch/0004-decoupled-chat-and-embedding-providers.md
// and internal/cli.validateProviderConfig, which enforces this at config
// load time so this path should be unreachable in practice.
func (p *ClaudeProvider) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error) {
	return nil, fmt.Errorf("ClaudeProvider does not support embeddings: Claude has no embeddings API; configure vector_store.provider to an embedding-capable provider (openai, ollama, gemini, or voyage)")
}

func (p *ClaudeProvider) CountTokens(ctx context.Context, text string) (int, error) {
	resp, err := p.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model: anthropic.Model(p.model),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("claude count_tokens request failed: %w", err)
	}
	return int(resp.InputTokens), nil
}
