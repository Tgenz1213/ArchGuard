package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type OpenAIProvider struct {
	apiKey     string
	model      string
	embedModel string
	client     *http.Client
}

func NewOpenAIProvider(apiKey, model, embedModel string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:     apiKey,
		model:      model,
		embedModel: embedModel,
		client:     &http.Client{},
	}
}

func (p *OpenAIProvider) Chat(ctx context.Context, system, user string) (string, error) {
	payload := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	var res struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}

	err := p.post(ctx, "https://api.openai.com/v1/chat/completions", payload, &res)
	if err != nil {
		return "", err
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}
	return res.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) post(ctx context.Context, url string, body interface{}, target interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func (p *OpenAIProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	reqBody := map[string]interface{}{
		"input": text,
		"model": p.embedModel,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embedding api error: %s", resp.Status)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embedding data returned")
	}

	return result.Data[0].Embedding, nil
}
