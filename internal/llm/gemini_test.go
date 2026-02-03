package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeminiProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-1.5-flash:generateContent" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-api-key" {
			t.Errorf("Unexpected API key: %s", r.URL.Query().Get("key"))
		}

		resp := struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}{
			Candidates: []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			}{
				{
					Content: struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text string `json:"text"`
						}{
							{Text: "{\"violation\": false}"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:  "test-api-key",
		model:   "gemini-1.5-flash",
		baseURL: server.URL,
		client:  server.Client(),
	}

	res, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	expected := "{\"violation\": false}"
	if res != expected {
		t.Errorf("Expected %s, got %s", expected, res)
	}
}

func TestGeminiProvider_CreateEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/text-embedding-004:embedContent" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}

		resp := struct {
			Embedding struct {
				Values []float32 `json:"values"`
			} `json:"embedding"`
		}{
			Embedding: struct {
				Values []float32 `json:"values"`
			}{
				Values: []float32{0.1, 0.2, 0.3},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:     "test-api-key",
		embedModel: "text-embedding-004",
		baseURL:    server.URL,
		client:     server.Client(),
	}

	res, err := p.CreateEmbedding(context.Background(), "test text")
	if err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}

	expected := []float32{0.1, 0.2, 0.3}
	if len(res) != len(expected) {
		t.Fatalf("Expected length %d, got %d", len(expected), len(res))
	}
	for i := range res {
		if res[i] != expected[i] {
			t.Errorf("At index %d: expected %f, got %f", i, expected[i], res[i])
		}
	}
}
