package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	u, err := url.Parse(fmt.Sprintf("%s/v1beta/models/%s:generateContent", p.baseURL, p.model))
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	q := u.Query()
	q.Set("key", p.apiKey)
	u.RawQuery = q.Encode()
	reqURL := u.String()

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

	err = p.post(ctx, reqURL, payload, &res)
	if err != nil {
		return "", err
	}

	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no candidates or parts")
	}

	return res.Candidates[0].Content.Parts[0].Text, nil
}

func (p *GeminiProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	u, err := url.Parse(fmt.Sprintf("%s/v1beta/models/%s:embedContent", p.baseURL, p.embedModel))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	q := u.Query()
	q.Set("key", p.apiKey)
	u.RawQuery = q.Encode()
	reqURL := u.String()

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

	err = p.post(ctx, reqURL, payload, &res)
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// Read the response body first so we can include it in the error if needed
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("gemini api error (%s): failed to read response body: %w", resp.Status, readErr)
		}

		// Try to decode structured error response
		var errRes struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if decodeErr := json.Unmarshal(body, &errRes); decodeErr != nil || errRes.Error.Message == "" {
			// If decode fails or message is empty, return error with raw body
			return fmt.Errorf("gemini api error (%s): %s", resp.Status, string(body))
		}
		return fmt.Errorf("gemini api error (%s): %s", resp.Status, errRes.Error.Message)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
