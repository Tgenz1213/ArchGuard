package llm

import (
	"context"
	"encoding/json"
	"fmt"
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

	res, err := p.CreateEmbedding(context.Background(), "test text", EmbeddingTaskDocument)
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

// TestOllamaProvider_CreateEmbedding_NomicTaskPrefix asserts the
// search_query:/search_document: prefix is applied only for a
// nomic-embed-text embedding model, and only with the prefix matching the
// requested task role -- gated so it doesn't corrupt embeddings for any
// other Ollama embedding model, which has no such text convention.
func TestOllamaProvider_CreateEmbedding_NomicTaskPrefix(t *testing.T) {
	cases := []struct {
		name       string
		embedModel string
		task       EmbeddingTaskType
		wantPrompt string
	}{
		{"nomic + document task", "nomic-embed-text", EmbeddingTaskDocument, "search_document: test text"},
		{"nomic + query task", "nomic-embed-text", EmbeddingTaskQuery, "search_query: test text"},
		{"nomic versioned tag + query task", "nomic-embed-text:v1.5", EmbeddingTaskQuery, "search_query: test text"},
		{"registry-qualified nomic model still matches", "my-registry:5000/nomic-embed-text", EmbeddingTaskQuery, "search_query: test text"},
		{"namespaced nomic model still matches", "library/nomic-embed-text", EmbeddingTaskDocument, "search_document: test text"},
		{"non-nomic model unprefixed", "mxbai-embed-large", EmbeddingTaskQuery, "test text"},
		{"registry-qualified non-nomic model unprefixed", "my-registry:5000/mxbai-embed-large", EmbeddingTaskQuery, "test text"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotPrompt string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reqBody map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				gotPrompt, _ = reqBody["prompt"].(string)

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"embedding":[0.1]}`))
			}))
			defer server.Close()

			p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", c.embedModel, 0.0)

			if _, err := p.CreateEmbedding(context.Background(), "test text", c.task); err != nil {
				t.Fatalf("CreateEmbedding failed: %v", err)
			}
			if gotPrompt != c.wantPrompt {
				t.Errorf("expected prompt %q, got %q", c.wantPrompt, gotPrompt)
			}
		})
	}
}

func TestNewOllamaProvider_DefaultsBaseURL(t *testing.T) {
	p := NewOllamaProvider("", "llama3.2", "nomic-embed-text", 0.0)
	if p.host != "http://localhost:11434" {
		t.Errorf("expected default host http://localhost:11434, got %q", p.host)
	}
}

func TestOllamaProvider_CountTokens(t *testing.T) {
	const representativeString = "Hello, world!"
	// Ollama's own tokenizer, not cl100k_base, produces this count: 5 tokens
	// for "Hello, world!" under llama3.2, vs. cl100k_base's 4. Captured from
	// a real local `llama3.2` server via /api/generate with num_predict:1.
	const realPromptEvalCount = 5

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected /api/generate, got %s", r.URL.Path)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "llama3.2" {
			t.Errorf("expected model llama3.2, got %v", reqBody["model"])
		}
		if reqBody["prompt"] != representativeString {
			t.Errorf("expected prompt %q, got %v", representativeString, reqBody["prompt"])
		}
		if reqBody["raw"] != true {
			t.Errorf("expected raw=true, got %v", reqBody["raw"])
		}
		options, ok := reqBody["options"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected options object, got %v", reqBody["options"])
		}
		if options["num_predict"] != float64(1) {
			t.Errorf("expected num_predict=1, got %v", options["num_predict"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"done":true,"response":"","prompt_eval_count":%d}`, realPromptEvalCount)
	}))
	defer server.Close()

	p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text", 0.0)

	n, err := p.CountTokens(context.Background(), representativeString)
	if err != nil {
		t.Fatalf("CountTokens failed: %v", err)
	}
	if n != realPromptEvalCount {
		t.Errorf("expected %d tokens (real llama3.2 count), got %d", realPromptEvalCount, n)
	}
}
