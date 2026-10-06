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

const maxVoyageErrorBodyBytes = 4096

const voyageBaseURL = "https://api.voyageai.com"
const defaultVoyageModel = "voyage-4"

type VoyageProvider struct {
	apiKey     string
	embedModel string
	baseURL    string
	client     *http.Client
}

func NewVoyageProvider(apiKey, embedModel string) *VoyageProvider {
	return NewVoyageProviderWithBaseURL(apiKey, embedModel, voyageBaseURL, &http.Client{})
}

func NewVoyageProviderWithBaseURL(apiKey, embedModel, baseURL string, httpClient *http.Client) *VoyageProvider {
	if embedModel == "" {
		embedModel = defaultVoyageModel
	}

	return &VoyageProvider{
		apiKey:     apiKey,
		embedModel: embedModel,
		baseURL:    baseURL,
		client:     httpClient,
	}
}

func voyageInputType(task EmbeddingTaskType) string {
	return task.Pick("document", "query")
}

// voyage-context-* models only work with contextualized_embed(), sent as a single chunk.
func (p *VoyageProvider) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error) {
	if strings.HasPrefix(p.embedModel, "voyage-context-") {
		return p.contextualizedEmbed(ctx, text, task)
	}

	return p.embed(ctx, text, task)
}

func (p *VoyageProvider) embed(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error) {
	reqBody := map[string]any{
		"input":      []string{text},
		"model":      p.embedModel,
		"input_type": voyageInputType(task),
	}

	var respBody struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := p.doRequest(ctx, "/v1/embeddings", reqBody, &respBody); err != nil {
		return nil, err
	}

	if len(respBody.Data) == 0 {
		return nil, fmt.Errorf("voyage returned no embedding data")
	}

	return respBody.Data[0].Embedding, nil
}

func (p *VoyageProvider) contextualizedEmbed(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error) {
	reqBody := map[string]any{
		"inputs":     [][]string{{text}},
		"model":      p.embedModel,
		"input_type": voyageInputType(task),
	}

	var respBody struct {
		Data []struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := p.doRequest(ctx, "/v1/contextualizedembeddings", reqBody, &respBody); err != nil {
		return nil, err
	}

	if len(respBody.Data) == 0 || len(respBody.Data[0].Data) == 0 {
		return nil, fmt.Errorf("voyage returned no embedding data")
	}

	return respBody.Data[0].Data[0].Embedding, nil
}

func (p *VoyageProvider) doRequest(ctx context.Context, path string, reqBody, respBody any) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal voyage request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to build voyage request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("voyage request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // closing a read response body can't change the result

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxVoyageErrorBodyBytes))
		if readErr != nil || len(body) == 0 {
			return fmt.Errorf("voyage api error: %s", resp.Status)
		}

		return fmt.Errorf("voyage api error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("failed to decode voyage response: %w", err)
	}

	return nil
}
