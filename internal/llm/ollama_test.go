package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "llama3.2" {
			t.Errorf("expected model llama3.2, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"{\"violation\": false}"},"done":true}`))
	}))
	defer server.Close()

	p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text", 0.0)

	res, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if res != `{"violation": false}` {
		t.Errorf("unexpected response: %q", res)
	}
}

func TestOllamaProvider_CreateEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "nomic-embed-text" {
			t.Errorf("expected model nomic-embed-text, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer server.Close()

	p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text", 0.0)

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

func TestNewOllamaProvider_DefaultsBaseURL(t *testing.T) {
	p := NewOllamaProvider("", "llama3.2", "nomic-embed-text", 0.0)
	if p.host != "http://localhost:11434" {
		t.Errorf("expected default host http://localhost:11434, got %q", p.host)
	}
}
