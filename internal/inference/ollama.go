package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxOllamaErrorBodyBytes = 4096

type OllamaProvider struct {
	temperature
	host       string
	model      string
	embedModel string
	client     *http.Client
}

func NewOllamaProvider(baseURL, model, embedModel string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	return newOllamaProvider(baseURL, model, embedModel)
}

// Unlike NewOllamaProvider, never defaults an empty baseURL to localhost.
func NewOllamaProviderWithBaseURL(baseURL, model, embedModel string) *OllamaProvider {
	return newOllamaProvider(baseURL, model, embedModel)
}

func newOllamaProvider(baseURL, model, embedModel string) *OllamaProvider {
	return &OllamaProvider{
		host:       baseURL,
		model:      model,
		embedModel: embedModel,
		client:     http.DefaultClient,
	}
}

func (p *OllamaProvider) Chat(ctx context.Context, system, user string) (ChatReply, error) {
	options := map[string]any{}

	if value := p.Temperature(); value != nil {
		options["temperature"] = *value
	}

	req := map[string]any{
		"model":   p.model,
		"stream":  false,
		"format":  "json",
		"options": options,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}

	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done            bool `json:"done"`
		PromptEvalCount int  `json:"prompt_eval_count"`
		EvalCount       int  `json:"eval_count"`
	}
	if err := p.post(ctx, "/api/chat", req, &resp); err != nil {
		return ChatReply{}, err
	}

	usage := Usage{InputTokens: resp.PromptEvalCount, OutputTokens: resp.EvalCount, Reported: resp.Done}

	return ChatReply{Text: resp.Message.Content, Usage: usage}, nil
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

func (p *OllamaProvider) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) (Embedding, error) {
	req := map[string]any{
		"model":  p.embedModel,
		"prompt": embeddingTaskPrefix(p.embedModel, task) + text,
	}

	var resp struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := p.post(ctx, "/api/embeddings", req, &resp); err != nil {
		return Embedding{}, err
	}

	// Ollama's /api/embeddings endpoint reports no token count.
	return Embedding{Vector: resp.Embedding}, nil
}

func (p *OllamaProvider) CountTokens(ctx context.Context, text string) (int, error) {
	req := map[string]any{
		"model":  p.model,
		"prompt": text,
		"raw":    true,
		"stream": false,
		"options": map[string]any{
			"num_predict": 1,
		},
	}

	var resp struct {
		PromptEvalCount int `json:"prompt_eval_count"`
	}
	if err := p.post(ctx, "/api/generate", req, &resp); err != nil {
		return 0, err
	}

	return resp.PromptEvalCount, nil
}

func (p *OllamaProvider) post(ctx context.Context, path string, reqBody, respBody any) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.host, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to build ollama request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // closing a read response body can't change the result

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ollamaStatusError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read ollama response: %w", err)
	}

	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &failure) == nil && failure.Error != "" {
		return fmt.Errorf("ollama api error: %s", failure.Error)
	}

	if err := json.Unmarshal(body, respBody); err != nil {
		return fmt.Errorf("failed to decode ollama response: %w", err)
	}

	return nil
}

// Ollama reports failures as {"error": "..."}, e.g. a model that was never pulled.
func ollamaStatusError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOllamaErrorBodyBytes))
	if err != nil || len(body) == 0 {
		return fmt.Errorf("ollama api error: %s", resp.Status)
	}

	var decoded struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &decoded) == nil && decoded.Error != "" {
		return fmt.Errorf("ollama api error: %s: %s", resp.Status, decoded.Error)
	}

	return fmt.Errorf("ollama api error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
}
