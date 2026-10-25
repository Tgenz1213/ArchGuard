package inference

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["model"] != "gpt-4o-mini" {
			t.Errorf("expected model gpt-4o-mini, got %v", reqBody["model"])
		}

		messages, ok := reqBody["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %v", reqBody["messages"])
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{"choices":[{"message":{"content":"{\"violation\": false}"}}]}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	p := NewOpenAIProviderWithBaseURL("test-api-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())

	res, err := p.Chat(t.Context(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if res.Text != `{"violation": false}` {
		t.Errorf("unexpected response: %q", res.Text)
	}
}

func TestOpenAIProvider_Chat_Temperature(t *testing.T) {
	tests := []struct {
		name string
		set  *float64
		want *float64
	}{
		{"unset", nil, nil},
		{"zero", new(0.0), new(0.0)},
		{"explicit", new(0.2), new(0.2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, body := serveCapturing(t, `{"choices":[{"message":{"content":"{}"}}]}`)

			p := NewOpenAIProviderWithBaseURL("test-api-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())
			p.SetTemperature(tt.set)

			if _, err := p.Chat(t.Context(), "system", "user"); err != nil {
				t.Fatalf("Chat failed: %v", err)
			}

			got, present := body()["temperature"]
			if present != (tt.want != nil) {
				t.Fatalf("temperature present = %v, want %v", present, tt.want != nil)
			}

			if present && got != *tt.want {
				t.Errorf("temperature = %v, want %v", got, *tt.want)
			}
		})
	}
}

func TestOpenAIProvider_CreateEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "embeddings") {
			t.Errorf("expected embeddings path, got %s", r.URL.Path)
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["input"] != "test text" {
			t.Errorf("expected input 'test text', got %v", reqBody["input"])
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOpenAIProviderWithBaseURL("test-api-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())

	res, err := provider.CreateEmbedding(t.Context(), "test text", EmbeddingTaskDocument)
	if err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}

	expected := []float32{0.1, 0.2, 0.3}
	if !slices.Equal(res.Vector, expected) {
		t.Errorf("vector = %v, want %v", res.Vector, expected)
	}
}

func TestOpenAIProvider_ChatErrorOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)

		if _, err := w.Write([]byte(`{"error":{"message":"invalid api key"}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	p := NewOpenAIProviderWithBaseURL("bad-key", "gpt-4o-mini", "text-embedding-3-small", server.URL, server.Client())

	_, err := p.Chat(t.Context(), "system", "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOpenAIProvider_CountTokens(t *testing.T) {
	// "Hello, world!" is 4 tokens under cl100k_base: ["Hello", ",", " world", "!"].
	tests := []struct {
		name  string
		model string
	}{
		{name: "known model resolves to cl100k_base", model: "gpt-3.5-turbo"},
		{name: "unknown model falls back to cl100k_base", model: "some-future-model-tiktoken-does-not-know"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewOpenAIProvider("unused-key", tt.model, "unused-embed-model")

			count, err := provider.CountTokens(t.Context(), "Hello, world!")
			if err != nil {
				t.Fatalf("CountTokens failed: %v", err)
			}

			if count != 4 {
				t.Errorf("expected 4 tokens, got %d", count)
			}
		})
	}
}
