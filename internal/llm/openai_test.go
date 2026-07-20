package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "chat/completions") {
			t.Errorf("expected chat/completions path, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("expected Bearer auth header, got %q", got)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "gpt-4o-mini" {
			t.Errorf("expected model gpt-4o-mini, got %v", reqBody["model"])
		}
		messages, ok := reqBody["messages"].([]interface{})
		if !ok || len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %v", reqBody["messages"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"violation\": false}"}}]}`))
	}))
	defer server.Close()

	p := NewOpenAIProviderWithBaseURL("test-api-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())

	res, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if res != `{"violation": false}` {
		t.Errorf("unexpected response: %q", res)
	}
}

func TestOpenAIProvider_CreateEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "embeddings") {
			t.Errorf("expected embeddings path, got %s", r.URL.Path)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["input"] != "test text" {
			t.Errorf("expected input 'test text', got %v", reqBody["input"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()

	p := NewOpenAIProviderWithBaseURL("test-api-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())

	res, err := p.CreateEmbedding(context.Background(), "test text")
	if err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
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

func TestOpenAIProvider_ChatErrorOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	p := NewOpenAIProviderWithBaseURL("bad-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())

	_, err := p.Chat(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
