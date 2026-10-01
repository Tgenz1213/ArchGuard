package llm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const claudeBaseURL = "https://api.anthropic.com"

// Not configurable: prompts always ask for a short JSON object.
const claudeMaxResponseTokens = 1024

type ClaudeProvider struct {
	client anthropic.Client
	model  string
}

func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
	return NewClaudeProviderWithBaseURL(apiKey, model, claudeBaseURL, &http.Client{})
}

// NewClaudeProviderWithBaseURL lets tests inject an httptest.Server.
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
