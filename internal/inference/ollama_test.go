package inference

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func serveCapturing(t *testing.T, response string) (*httptest.Server, func() map[string]any) {
	t.Helper()

	var (
		mu   sync.Mutex
		body map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded map[string]any
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			t.Errorf("decoding request body: %v", err)
		}

		mu.Lock()
		body = decoded
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(response)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()

		return body
	}
}

func TestOllamaProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["model"] != "llama3.2" {
			t.Errorf("expected model llama3.2, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{"message":{"role":"assistant","content":"{\"violation\": false}"},"done":true}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text")

	res, err := p.Chat(t.Context(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if res != `{"violation": false}` {
		t.Errorf("unexpected response: %q", res)
	}
}

func TestOllamaProvider_CreateEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["model"] != "nomic-embed-text" {
			t.Errorf("expected model nomic-embed-text, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text")

	res, err := provider.CreateEmbedding(t.Context(), "test text", EmbeddingTaskDocument)
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
				var reqBody map[string]any
				if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}

				gotPrompt, _ = reqBody["prompt"].(string)

				w.Header().Set("Content-Type", "application/json")

				if _, err := w.Write([]byte(`{"embedding":[0.1]}`)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			}))
			defer server.Close()

			p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", c.embedModel)

			if _, err := p.CreateEmbedding(t.Context(), "test text", c.task); err != nil {
				t.Fatalf("CreateEmbedding failed: %v", err)
			}

			if gotPrompt != c.wantPrompt {
				t.Errorf("expected prompt %q, got %q", c.wantPrompt, gotPrompt)
			}
		})
	}
}

func TestNewOllamaProvider_DefaultsBaseURL(t *testing.T) {
	p := NewOllamaProvider("", "llama3.2", "nomic-embed-text")
	if p.host != "http://localhost:11434" {
		t.Errorf("expected default host http://localhost:11434, got %q", p.host)
	}
}

func TestOllamaProvider_CountTokens(t *testing.T) {
	const representativeString = "Hello, world!"
	const realPromptEvalCount = 5 // llama3.2's real tokenizer count, captured live

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected /api/generate, got %s", r.URL.Path)
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["model"] != "llama3.2" {
			t.Errorf("expected model llama3.2, got %v", reqBody["model"])
		}

		if reqBody["prompt"] != representativeString {
			t.Errorf("expected prompt %q, got %v", representativeString, reqBody["prompt"])
		}

		if raw, ok := reqBody["raw"].(bool); !ok || !raw {
			t.Errorf("expected raw=true, got %v", reqBody["raw"])
		}

		options, ok := reqBody["options"].(map[string]any)
		if !ok {
			t.Fatalf("expected options object, got %v", reqBody["options"])
		}

		if options["num_predict"] != float64(1) {
			t.Errorf("expected num_predict=1, got %v", options["num_predict"])
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := fmt.Fprintf(w, `{"done":true,"response":"","prompt_eval_count":%d}`, realPromptEvalCount); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text")

	count, err := provider.CountTokens(t.Context(), representativeString)
	if err != nil {
		t.Fatalf("CountTokens failed: %v", err)
	}

	if count != realPromptEvalCount {
		t.Errorf("expected %d tokens (real llama3.2 count), got %d", realPromptEvalCount, count)
	}
}

func TestOllamaProvider_Chat_Temperature(t *testing.T) {
	tests := []struct {
		name string
		set  *float64
		want *float64
	}{
		{"unset", nil, nil},
		{"zero", new(0.0), new(0.0)},
		{"explicit", new(0.4), new(0.4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, body := serveCapturing(t, `{"message":{"role":"assistant","content":"{}"},"done":true}`)

			p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text")
			p.SetTemperature(tt.set)

			if _, err := p.Chat(t.Context(), "system", "user"); err != nil {
				t.Fatalf("Chat failed: %v", err)
			}

			options, _ := body()["options"].(map[string]any)
			got, present := options["temperature"]

			if present != (tt.want != nil) {
				t.Fatalf("temperature present = %v, want %v (options %v)", present, tt.want != nil, options)
			}

			if present && got != *tt.want {
				t.Errorf("temperature = %v, want %v", got, *tt.want)
			}
		})
	}
}

func TestOllamaProvider_ErrorNamesOllamasError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"error field on a 200", http.StatusOK, `{"error":"unexpected server error"}`, "ollama api error: unexpected server error"},
		{"json error field", http.StatusNotFound, `{"error":"model \"llama3.2\" not found, try pulling it first"}`, `ollama api error: 404 Not Found: model "llama3.2" not found, try pulling it first`},
		{"plain body", http.StatusNotFound, "not found", "ollama api error: 404 Not Found: not found"},
		{"empty body", http.StatusNotFound, "", "ollama api error: 404 Not Found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)

				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			p := NewOllamaProviderWithBaseURL(server.URL, "llama3.2", "nomic-embed-text")

			_, err := p.Chat(t.Context(), "system", "user")
			if err == nil || err.Error() != tt.want {
				t.Errorf("Chat() error = %v, want %q", err, tt.want)
			}
		})
	}
}
