package inference

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

func NewOllamaProvider(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	return newOllamaProvider(baseURL, model, embedModel, temperature)
}

// Unlike NewOllamaProvider, never defaults an empty baseURL to localhost.
func NewOllamaProviderWithBaseURL(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	return newOllamaProvider(baseURL, model, embedModel, temperature)
}

func newOllamaProvider(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	base, err := url.Parse(baseURL)
	if err != nil {
		// Requests will fail with a descriptive error instead of panicking on a nil base URL.
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

	err := p.client.Chat(ctx, req, func(resp api.ChatResponse) error {
		content = resp.Message.Content
		return nil
	})
	if err != nil {
		return "", err
	}

	return content, nil
}

// embeddingPrefixConventions maps a model name prefix to its
// asymmetric-retrieval instruction prefixes; add entries here, not branches.
var embeddingPrefixConventions = []struct {
	modelPrefix    string
	documentPrefix string
	queryPrefix    string
}{
	{"nomic-embed", "search_document: ", "search_query: "},
}

// embeddingTaskPrefix returns "" if embedModel matches no known
// convention, matching against the last "/"-separated name segment.
func embeddingTaskPrefix(embedModel string, task EmbeddingTaskType) string {
	name := embedModel
	if i := strings.LastIndex(name, "/"); i != -1 {
		name = name[i+1:]
	}

	for _, c := range embeddingPrefixConventions {
		if strings.HasPrefix(name, c.modelPrefix) {
			return task.Pick(c.documentPrefix, c.queryPrefix)
		}
	}

	return ""
}

func (p *OllamaProvider) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error) {
	req := &api.EmbeddingRequest{
		Model:  p.embedModel,
		Prompt: embeddingTaskPrefix(p.embedModel, task) + text,
	}

	resp, err := p.client.Embeddings(ctx, req)
	if err != nil {
		return nil, err
	}

	embedding := make([]float32, len(resp.Embedding))
	for i, value := range resp.Embedding {
		embedding[i] = float32(value)
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

	err := p.client.Generate(ctx, req, func(resp api.GenerateResponse) error {
		count = resp.PromptEvalCount
		return nil
	})
	if err != nil {
		return 0, err
	}

	return count, nil
}
