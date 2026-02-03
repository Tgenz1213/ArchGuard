package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type GeminiProvider struct {
	apiKey     string
	model      string
	embedModel string
	baseURL    string
	client     *http.Client
}

func NewGeminiProvider(apiKey, model, embedModel string) *GeminiProvider {
	return &GeminiProvider{
		apiKey:     apiKey,
		model:      model,
		embedModel: embedModel,
		baseURL:    "https://generativelanguage.googleapis.com",
		client:     &http.Client{},
	}
}

func (p *GeminiProvider) Chat(ctx context.Context, system, user string) (string, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, p.model, p.apiKey)

	// Combine system and user prompts for Gemini
	fullPrompt := fmt.Sprintf("%s\n\n%s", system, user)

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": fullPrompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"response_mime_type": "application/json",
		},
	}

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	err := p.post(ctx, url, payload, &res)
	if err != nil {
		return "", err
	}

	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no candidates or parts")
	}

	return res.Candidates[0].Content.Parts[0].Text, nil
}

func (p *GeminiProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s:embedContent?key=%s", p.baseURL, p.embedModel, p.apiKey)

	payload := map[string]interface{}{
		"content": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": text},
			},
		},
	}

	var res struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}

	err := p.post(ctx, url, payload, &res)
	if err != nil {
		return nil, err
	}

	return res.Embedding.Values, nil
}

func (p *GeminiProvider) post(ctx context.Context, url string, body interface{}, target interface{}) error {
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
		var errRes struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errRes)
		return fmt.Errorf("gemini api error (%s): %s", resp.Status, errRes.Error.Message)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
