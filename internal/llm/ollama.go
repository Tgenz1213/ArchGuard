package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type OllamaProvider struct {
	host        string
	model       string
	embedModel  string
	temperature float64
	client      *http.Client
}

// NewOllamaProvider initializes the Ollama provider with necessary configuration.
func NewOllamaProvider(baseURL, model, embedModel string, temperature float64) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		host:        baseURL,
		model:       model,
		embedModel:  embedModel,
		temperature: temperature,
		client:      &http.Client{},
	}
}

/**
 * REGION: Interface Implementation
 */

func (p *OllamaProvider) Chat(ctx context.Context, system, user string) (string, error) {
	payload := map[string]interface{}{
		"model":  p.model,
		"format": "json",
		"stream": false,
		"options": map[string]interface{}{
			"temperature": p.temperature,
		},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}

	var res struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	err := p.post(ctx, p.host+"/api/chat", payload, &res)
	if err != nil {
		return "", err
	}
	return res.Message.Content, nil
}

func (p *OllamaProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]interface{}{
		"model":  p.embedModel,
		"prompt": text,
	}

	var res struct {
		Embedding []float32 `json:"embedding"`
	}

	err := p.post(ctx, p.host+"/api/embeddings", payload, &res)
	if err != nil {
		return nil, err
	}
	return res.Embedding, nil
}

/**
 * REGION: Internal Helpers
 */

func (p *OllamaProvider) post(ctx context.Context, url string, body interface{}, target interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama api error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
