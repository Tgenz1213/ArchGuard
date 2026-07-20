package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"google.golang.org/genai"
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

// errorCapturingTransport wraps an http.RoundTripper and remembers the
// status line and raw body of the most recent non-2xx response it saw. The
// genai SDK's own error type (genai.APIError) discards the HTTP status text
// and raw body whenever the response happens to parse as JSON with an
// "error" object (even if that object's "message" is empty), so we capture
// the response ourselves to preserve GeminiProvider's error-message
// contract: callers get both the HTTP status and whatever error detail the
// server sent, structured or not.
type errorCapturingTransport struct {
	base       http.RoundTripper
	lastStatus string
	lastBody   []byte
}

func (t *errorCapturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr == nil {
			t.lastStatus = resp.Status
			t.lastBody = body
		}
		// Restore the body so the genai SDK can still read and report on it.
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}

	return resp, nil
}

// newClient builds a genai.Client scoped to a single request/response cycle,
// configured from the provider's apiKey/baseURL/client fields. Building it
// per-call (rather than baking it into NewGeminiProvider) is what lets
// gemini_test.go construct &GeminiProvider{baseURL: server.URL, client:
// server.Client()} directly and have it work against an httptest.Server.
func (p *GeminiProvider) newClient(ctx context.Context) (*genai.Client, *errorCapturingTransport, error) {
	httpClient := p.client
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &errorCapturingTransport{base: base}

	wrapped := &http.Client{
		Transport:     transport,
		CheckRedirect: httpClient.CheckRedirect,
		Jar:           httpClient.Jar,
		Timeout:       httpClient.Timeout,
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     p.apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: wrapped,
		HTTPOptions: genai.HTTPOptions{
			BaseURL: p.baseURL,
		},
	})
	return client, transport, err
}

// apiError turns a failed SDK call into an error that preserves the
// provider's historical contract: include the HTTP status line, and prefer
// a structured "error.message" from the response body when present,
// otherwise fall back to the raw body.
func (p *GeminiProvider) apiError(err error, transport *errorCapturingTransport) error {
	if transport != nil && transport.lastStatus != "" {
		return buildAPIError(transport.lastStatus, transport.lastBody)
	}
	return fmt.Errorf("gemini api error: %w", err)
}

func buildAPIError(status string, body []byte) error {
	var errRes struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errRes); err != nil || errRes.Error.Message == "" {
		return fmt.Errorf("gemini api error (%s): %s", status, string(body))
	}
	return fmt.Errorf("gemini api error (%s): %s", status, errRes.Error.Message)
}

func (p *GeminiProvider) Chat(ctx context.Context, system, user string) (string, error) {
	client, transport, err := p.newClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create gemini client: %w", err)
	}

	// Combine system and user prompts for Gemini
	fullPrompt := fmt.Sprintf("%s\n\n%s", system, user)
	contents := []*genai.Content{genai.NewContentFromText(fullPrompt, genai.RoleUser)}
	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
	}

	resp, err := client.Models.GenerateContent(ctx, p.model, contents, config)
	if err != nil {
		return "", p.apiError(err, transport)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no candidates or parts")
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

func (p *GeminiProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	client, transport, err := p.newClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	contents := []*genai.Content{genai.NewContentFromText(text, genai.RoleUser)}

	resp, err := client.Models.EmbedContent(ctx, p.embedModel, contents, nil)
	if err != nil {
		return nil, p.apiError(err, transport)
	}

	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("gemini returned no embeddings")
	}

	return resp.Embeddings[0].Values, nil
}
