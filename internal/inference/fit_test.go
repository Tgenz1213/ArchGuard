package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const mockedInputLimit = 100

// Each rejection body is what the real API returned for an over-long input.
func TestEmbedToFit_ShortensUntilEachProviderAccepts(t *testing.T) {
	tests := []struct {
		name     string
		input    func(body map[string]any) string
		reject   func(w http.ResponseWriter) error
		accept   string
		embedder func(url string, client *http.Client) Embedder
	}{
		{
			name:   "ollama",
			input:  func(body map[string]any) string { return body["prompt"].(string) },
			reject: rejectWith(http.StatusInternalServerError, `{"error":"the input length exceeds the context length"}`),
			accept: `{"embedding":[1,0]}`,
			embedder: func(url string, _ *http.Client) Embedder {
				return NewOllamaProviderWithBaseURL(url, "llama3.2", "mxbai-embed-large")
			},
		},
		{
			name:   "openai",
			input:  func(body map[string]any) string { return body["input"].(string) },
			reject: rejectWith(http.StatusBadRequest, `{"error":{"message":"Invalid 'input': maximum context length is 8192 tokens.","type":"invalid_request_error","param":null,"code":null}}`),
			accept: `{"data":[{"embedding":[1,0]}]}`,
			embedder: func(url string, client *http.Client) Embedder {
				return NewOpenAIProviderWithBaseURL("key", "gpt-4o-mini", "text-embedding-3-small", url, client)
			},
		},
		{
			name:   "voyage contextualized",
			input:  func(body map[string]any) string { return body["inputs"].([]any)[0].([]any)[0].(string) },
			reject: rejectWith(http.StatusBadRequest, `{"detail":"Request to model 'voyage-context-3' failed. The example at index 0 in your batch has too many tokens and does not fit into the model's context window of 32000 tokens."}`),
			accept: `{"data":[{"data":[{"embedding":[1,0]}]}]}`,
			embedder: func(url string, client *http.Client) Embedder {
				return NewVoyageProviderWithBaseURL("key", "voyage-context-3", url, client)
			},
		},
	}

	original := strings.Repeat("a line of code\n", 40)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decoding request: %v", err)
				}

				if len(tc.input(body)) > mockedInputLimit {
					if err := tc.reject(w); err != nil {
						t.Errorf("writing rejection: %v", err)
					}

					return
				}

				w.Header().Set("Content-Type", "application/json")

				if _, err := w.Write([]byte(tc.accept)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			}))
			defer server.Close()

			embedding, embedded, err := EmbedToFit(t.Context(), tc.embedder(server.URL, server.Client()), original, EmbeddingTaskQuery)
			if err != nil {
				t.Fatalf("EmbedToFit: %v", err)
			}

			if len(embedding) != 2 {
				t.Errorf("embedding length %d, want 2", len(embedding))
			}

			if len(embedded) > mockedInputLimit || !strings.HasPrefix(original, embedded) {
				t.Errorf("embedded %d characters, want a prefix of the input within %d", len(embedded), mockedInputLimit)
			}
		})
	}
}

func TestEmbedToFit_ReturnsOtherErrorsWithoutRetrying(t *testing.T) {
	var calls atomic.Int32
	boom := errors.New("boom")
	embedder := &MockProvider{EmbedFunc: func(_ context.Context, _ string, _ EmbeddingTaskType) ([]float32, error) {
		calls.Add(1)
		return nil, boom
	}}

	if _, _, err := EmbedToFit(t.Context(), embedder, "text", EmbeddingTaskQuery); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestEmbedToFit_GivesUpWhenNothingFits(t *testing.T) {
	var calls atomic.Int32
	embedder := &MockProvider{EmbedFunc: func(_ context.Context, _ string, _ EmbeddingTaskType) ([]float32, error) {
		calls.Add(1)
		return nil, ErrInputTooLong
	}}

	if _, _, err := EmbedToFit(t.Context(), embedder, strings.Repeat("x", 100000), EmbeddingTaskQuery); !errors.Is(err, ErrInputTooLong) {
		t.Fatalf("err = %v, want ErrInputTooLong", err)
	}

	if calls.Load() != maxShortenings+1 {
		t.Errorf("calls = %d, want %d", calls.Load(), maxShortenings+1)
	}
}

func rejectWith(status int, body string) func(w http.ResponseWriter) error {
	return func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, err := w.Write([]byte(body))

		return err
	}
}
