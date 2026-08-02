package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClaudeProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "claude-sonnet-4-5" {
			t.Errorf("expected model claude-sonnet-4-5, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "{\"violation\": false}"}],
			"model": "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	p := NewClaudeProviderWithBaseURL("test-api-key", "claude-sonnet-4-5", server.URL, server.Client())

	res, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if res != `{"violation": false}` {
		t.Errorf("unexpected response: %q", res)
	}
}

func TestClaudeProvider_CountTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" {
			t.Errorf("expected /v1/messages/count_tokens, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens": 42}`))
	}))
	defer server.Close()

	p := NewClaudeProviderWithBaseURL("test-api-key", "claude-sonnet-4-5", server.URL, server.Client())

	n, err := p.CountTokens(context.Background(), "some text to count")
	if err != nil {
		t.Fatalf("CountTokens failed: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42 tokens, got %d", n)
	}
}

func TestClaudeProvider_CreateEmbedding_ReturnsError(t *testing.T) {
	p := NewClaudeProvider("test-api-key", "claude-sonnet-4-5")
	if _, err := p.CreateEmbedding(context.Background(), "text", EmbeddingTaskQuery); err == nil {
		t.Fatal("expected CreateEmbedding to return an error, got nil")
	}
}
