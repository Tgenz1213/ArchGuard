package inference

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"github.com/pkoukk/tiktoken-go"
)

const openAIBaseURL = "https://api.openai.com/v1"

type OpenAIProvider struct {
	temperature
	client     openai.Client
	model      string
	embedModel string
}

func NewOpenAIProvider(apiKey, model, embedModel string) *OpenAIProvider {
	return NewOpenAIProviderWithBaseURL(apiKey, model, embedModel, openAIBaseURL, &http.Client{})
}

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

func (p *OpenAIProvider) Chat(ctx context.Context, system, user string) (ChatReply, error) {
	params := openai.ChatCompletionNewParams{
		Model: p.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
	}

	if value := p.Temperature(); value != nil {
		params.Temperature = openai.Float(*value)
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return ChatReply{}, fmt.Errorf("openai chat completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return ChatReply{}, fmt.Errorf("no choices returned")
	}

	usage := Usage{
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: int(resp.Usage.CompletionTokens),
		Reported:     resp.Usage.JSON.PromptTokens.Valid() && resp.Usage.JSON.CompletionTokens.Valid(),
	}

	return ChatReply{Text: resp.Choices[0].Message.Content, Usage: usage}, nil
}

func (p *OpenAIProvider) CreateEmbedding(ctx context.Context, text string, _ EmbeddingTaskType) (Embedding, error) {
	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
		Model: p.embedModel,
	})
	if err != nil {
		var apiErr *openai.Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Message, "maximum context length") {
			return Embedding{}, fmt.Errorf("openai embedding request failed: %w: %w", ErrInputTooLong, err)
		}

		return Embedding{}, fmt.Errorf("openai embedding request failed: %w", err)
	}

	if len(resp.Data) == 0 {
		return Embedding{}, fmt.Errorf("no embedding data returned")
	}

	src := resp.Data[0].Embedding
	embedding := make([]float32, len(src))
	for i, value := range src {
		embedding[i] = float32(value)
	}

	usage := Usage{InputTokens: int(resp.Usage.PromptTokens), Reported: resp.Usage.JSON.PromptTokens.Valid()}

	return Embedding{Vector: embedding, Usage: usage}, nil
}

func (p *OpenAIProvider) CountTokens(ctx context.Context, text string) (int, error) {
	model := p.model
	if model == "" {
		model = "gpt-3.5-turbo"
	}

	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		// Fallback to cl100k_base for unknown OpenAI model names.
		tkm, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return 0, fmt.Errorf("failed to load cl100k_base tokenizer: %w", err)
		}
	}

	return len(tkm.Encode(text, nil, nil)), nil
}
