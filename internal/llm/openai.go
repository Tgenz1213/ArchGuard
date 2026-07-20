package llm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const openAIBaseURL = "https://api.openai.com/v1"

type OpenAIProvider struct {
	client     openai.Client
	model      string
	embedModel string
}

// NewOpenAIProvider constructs an OpenAIProvider that talks to the real
// OpenAI API.
func NewOpenAIProvider(apiKey, model, embedModel string) *OpenAIProvider {
	return NewOpenAIProviderWithBaseURL(apiKey, model, embedModel, openAIBaseURL, &http.Client{})
}

// NewOpenAIProviderWithBaseURL constructs an OpenAIProvider pointed at a
// custom base URL using a custom HTTP client. This exists primarily so tests
// can inject an httptest.Server instead of hitting the real OpenAI API.
func NewOpenAIProviderWithBaseURL(apiKey, model, embedModel, baseURL string, httpClient *http.Client) *OpenAIProvider {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
	)
	return &OpenAIProvider{
		client:     client,
		model:      model,
		embedModel: embedModel,
	}
}

func (p *OpenAIProvider) Chat(ctx context.Context, system, user string) (string, error) {
	resp, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: p.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai chat completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}
	return resp.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
		Model: p.embedModel,
	})
	if err != nil {
		return nil, fmt.Errorf("openai embedding request failed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embedding data returned")
	}

	src := resp.Data[0].Embedding
	embedding := make([]float32, len(src))
	for i, v := range src {
		embedding[i] = float32(v)
	}
	return embedding, nil
}
