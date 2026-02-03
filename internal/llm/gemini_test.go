package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/v1beta/models/gemini-1.5-flash:generateContent" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-api-key" {
			t.Errorf("Unexpected API key: %s", r.URL.Query().Get("key"))
		}

		// Validate request body
		var reqBody struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				ResponseMimeType string `json:"response_mime_type"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		if len(reqBody.Contents) == 0 {
			t.Error("Request body missing contents")
		}
		if len(reqBody.Contents[0].Parts) == 0 {
			t.Error("Request body missing parts")
		}
		expectedPrompt := "system prompt\n\nuser prompt"
		if reqBody.Contents[0].Parts[0].Text != expectedPrompt {
			t.Errorf("Expected prompt %q, got %q", expectedPrompt, reqBody.Contents[0].Parts[0].Text)
		}
		if reqBody.GenerationConfig.ResponseMimeType != "application/json" {
			t.Errorf("Expected response_mime_type 'application/json', got %q", reqBody.GenerationConfig.ResponseMimeType)
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
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/v1beta/models/text-embedding-004:embedContent" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-api-key" {
			t.Errorf("Unexpected API key: %s", r.URL.Query().Get("key"))
		}

		// Validate request body
		var reqBody struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		if len(reqBody.Content.Parts) == 0 {
			t.Error("Request body missing parts")
		}
		if reqBody.Content.Parts[0].Text != "test text" {
			t.Errorf("Expected text 'test text', got %q", reqBody.Content.Parts[0].Text)
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

func TestGeminiProvider_URLEncoding(t *testing.T) {
	// Test that API keys with special characters are properly URL-encoded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that the API key is properly received (decoded by the server)
		key := r.URL.Query().Get("key")
		if key != "test+key&with=special%chars" {
			t.Errorf("API key not properly encoded/decoded. Expected 'test+key&with=special%%chars', got: %s", key)
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
							{Text: "{}"},
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
		apiKey:  "test+key&with=special%chars",
		model:   "gemini-1.5-flash",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := p.Chat(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("Chat failed with special characters in API key: %v", err)
	}
}

func TestGeminiProvider_ErrorHandling_StructuredError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:  "test-api-key",
		model:   "gemini-1.5-flash",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "Invalid API key") {
		t.Errorf("Expected error to contain 'Invalid API key', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "400 Bad Request") {
		t.Errorf("Expected error to contain status code, got: %s", errMsg)
	}
}

func TestGeminiProvider_ErrorHandling_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`This is not valid JSON`))
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:  "test-api-key",
		model:   "gemini-1.5-flash",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "This is not valid JSON") {
		t.Errorf("Expected error to contain raw body, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "500 Internal Server Error") {
		t.Errorf("Expected error to contain status code, got: %s", errMsg)
	}
}

func TestGeminiProvider_ErrorHandling_EmptyErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error": {"message": ""}}`))
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey:  "test-api-key",
		model:   "gemini-1.5-flash",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := p.Chat(context.Background(), "system prompt", "user prompt")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errMsg := err.Error()
	// Should include the raw body when message is empty
	if !strings.Contains(errMsg, `{"error": {"message": ""}}`) {
		t.Errorf("Expected error to contain raw body when message is empty, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "403 Forbidden") {
		t.Errorf("Expected error to contain status code, got: %s", errMsg)
	}
}
