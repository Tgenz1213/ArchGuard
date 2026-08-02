package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoyageProvider_CreateEmbedding_StandardModel(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("expected Bearer auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3],"index":0}],"model":"voyage-4","usage":{"total_tokens":5}}`))
	}))
	defer server.Close()

	p := NewVoyageProviderWithBaseURL("test-api-key", "voyage-4", server.URL, server.Client())

	res, err := p.CreateEmbedding(context.Background(), "test text", EmbeddingTaskQuery)
	if err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("expected /v1/embeddings, got %s", gotPath)
	}
	if gotBody["input_type"] != "query" {
		t.Errorf("expected input_type query, got %v", gotBody["input_type"])
	}
	expected := []float32{0.1, 0.2, 0.3}
	if len(res) != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), len(res))
	}
	for i := range res {
		if res[i] != expected[i] {
			t.Errorf("at index %d: expected %f, got %f", i, expected[i], res[i])
		}
	}
}

func TestVoyageProvider_CreateEmbedding_ContextualizedModel(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"data":[{"embedding":[0.4,0.5],"index":0}],"index":0}],"model":"voyage-context-3","usage":{"total_tokens":5}}`))
	}))
	defer server.Close()

	p := NewVoyageProviderWithBaseURL("test-api-key", "voyage-context-3", server.URL, server.Client())

	res, err := p.CreateEmbedding(context.Background(), "test text", EmbeddingTaskDocument)
	if err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}
	if gotPath != "/v1/contextualizedembeddings" {
		t.Errorf("expected /v1/contextualizedembeddings, got %s", gotPath)
	}
	inputs, ok := gotBody["inputs"].([]interface{})
	if !ok || len(inputs) != 1 {
		t.Fatalf("expected inputs to be a single-chunk-list, got %v", gotBody["inputs"])
	}
	chunk, ok := inputs[0].([]interface{})
	if !ok || len(chunk) != 1 || chunk[0] != "test text" {
		t.Fatalf("expected single chunk [\"test text\"], got %v", inputs[0])
	}
	if gotBody["input_type"] != "document" {
		t.Errorf("expected input_type document, got %v", gotBody["input_type"])
	}
	expected := []float32{0.4, 0.5}
	if len(res) != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), len(res))
	}
	for i := range res {
		if res[i] != expected[i] {
			t.Errorf("at index %d: expected %f, got %f", i, expected[i], res[i])
		}
	}
}

func TestVoyageProvider_Chat_ReturnsError(t *testing.T) {
	p := NewVoyageProvider("test-api-key", "voyage-4")
	if _, err := p.Chat(context.Background(), "system", "user"); err == nil {
		t.Fatal("expected Chat to return an error, got nil")
	}
}

func TestVoyageProvider_CountTokens_ReturnsError(t *testing.T) {
	p := NewVoyageProvider("test-api-key", "voyage-4")
	if _, err := p.CountTokens(context.Background(), "text"); err == nil {
		t.Fatal("expected CountTokens to return an error, got nil")
	}
}

func TestVoyageProvider_DefaultsEmbedModel(t *testing.T) {
	p := NewVoyageProvider("test-api-key", "")
	if p.embedModel != "voyage-4" {
		t.Errorf("expected default embed model voyage-4, got %q", p.embedModel)
	}
}

// TestVoyageProvider_CreateEmbedding_ErrorIncludesResponseBody guards against
// doRequest discarding the response body on a non-2xx response. Voyage's API
// puts the actual failure cause (invalid model, bad auth, rate limit) in the
// JSON body, so the error returned to the caller must include it rather than
// just the bare HTTP status line.
func TestVoyageProvider_CreateEmbedding_ErrorIncludesResponseBody(t *testing.T) {
	const wantDetail = "model \"bogus-model\" is not a valid Voyage model name"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"` + wantDetail + `"}`))
	}))
	defer server.Close()

	p := NewVoyageProviderWithBaseURL("test-api-key", "voyage-4", server.URL, server.Client())

	_, err := p.CreateEmbedding(context.Background(), "test text", EmbeddingTaskQuery)
	if err == nil {
		t.Fatal("expected CreateEmbedding to return an error for a 400 response")
	}
	if !strings.Contains(err.Error(), wantDetail) {
		t.Errorf("expected error to contain response body detail %q, got: %v", wantDetail, err)
	}
}
