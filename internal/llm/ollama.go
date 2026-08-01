package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/ollama/ollama/api"
)

type OllamaProvider struct {
	host        string
	model       string
	embedModel  string
	temperature float64
	client      *api.Client
}

// NewOllamaProvider initializes the Ollama provider with necessary configuration.
func NewOllamaProvider(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return newOllamaProvider(baseURL, model, embedModel, temperature)
}

// NewOllamaProviderWithBaseURL initializes the Ollama provider pointed at the
// given baseURL verbatim, without defaulting an empty value to localhost.
// This exists so tests can point the provider at an httptest.Server.
func NewOllamaProviderWithBaseURL(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	return newOllamaProvider(baseURL, model, embedModel, temperature)
}

func newOllamaProvider(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	base, err := url.Parse(baseURL)
	if err != nil {
		// Fall back to a client with no configured host; requests will fail
		// with a descriptive error rather than panicking on a nil base URL.
		base = &url.URL{}
	}
	return &OllamaProvider{
		host:        baseURL,
		model:       model,
		embedModel:  embedModel,
		temperature: temperature,
		client:      api.NewClient(base, http.DefaultClient),
	}
}

/**
 * REGION: Interface Implementation
 */

func (p *OllamaProvider) Chat(ctx context.Context, system, user string) (string, error) {
	stream := false
	req := &api.ChatRequest{
		Model:  p.model,
		Stream: &stream,
		Format: json.RawMessage(`"json"`),
		Options: map[string]any{
			"temperature": p.temperature,
		},
		Messages: []api.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	var content string
	err := p.client.Chat(ctx, req, func(res api.ChatResponse) error {
		content = res.Message.Content
		return nil
	})
	if err != nil {
		return "", err
	}
	return content, nil
}

// nomicTaskPrefix returns nomic-embed-text's asymmetric-retrieval
// instruction prefix for task, or "" for any other embedding model --
// nomic is the only Ollama embedding model documented to use this text
// convention, so applying it to another model's input would just corrupt
// the embedding with irrelevant tokens.
func nomicTaskPrefix(embedModel string, task EmbeddingTaskType) string {
	if !strings.HasPrefix(embedModel, "nomic-embed") {
		return ""
	}
	if task == EmbeddingTaskQuery {
		return "search_query: "
	}
	return "search_document: "
}

func (p *OllamaProvider) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error) {
	req := &api.EmbeddingRequest{
		Model:  p.embedModel,
		Prompt: nomicTaskPrefix(p.embedModel, task) + text,
	}

	res, err := p.client.Embeddings(ctx, req)
	if err != nil {
		return nil, err
	}

	embedding := make([]float32, len(res.Embedding))
	for i, v := range res.Embedding {
		embedding[i] = float32(v)
	}
	return embedding, nil
}

func (p *OllamaProvider) CountTokens(ctx context.Context, text string) (int, error) {
	stream := false
	req := &api.GenerateRequest{
		Model:  p.model,
		Prompt: text,
		Raw:    true,
		Stream: &stream,
		Options: map[string]any{
			"num_predict": 1,
		},
	}

	var count int
	err := p.client.Generate(ctx, req, func(res api.GenerateResponse) error {
		count = res.PromptEvalCount
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
