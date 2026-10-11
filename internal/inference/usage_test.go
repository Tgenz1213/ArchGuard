package inference

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
)

type usageCase struct {
	name     string
	response string
	call     func(ctx context.Context, server *httptest.Server) (Usage, error)
	want     Usage
}

func chatUsage(chat Chatter) func(ctx context.Context, _ *httptest.Server) (Usage, error) {
	return func(ctx context.Context, _ *httptest.Server) (Usage, error) {
		reply, err := chat.Chat(ctx, "system", "user")
		return reply.Usage, err
	}
}

func claudeChat(ctx context.Context, server *httptest.Server) (Usage, error) {
	return chatUsage(NewClaudeProviderWithBaseURL("key", "claude-sonnet-4-5", server.URL, server.Client()))(ctx, server)
}

func openAIChat(ctx context.Context, server *httptest.Server) (Usage, error) {
	return chatUsage(NewOpenAIProviderWithBaseURL("key", "gpt-4o-mini", "", server.URL, server.Client()))(ctx, server)
}

func geminiChat(ctx context.Context, server *httptest.Server) (Usage, error) {
	return chatUsage(&GeminiProvider{apiKey: "key", model: "gemini-2.5-flash", baseURL: server.URL, client: server.Client()})(ctx, server)
}

func ollamaChat(ctx context.Context, server *httptest.Server) (Usage, error) {
	return chatUsage(NewOllamaProviderWithBaseURL(server.URL, "llama3.2", ""))(ctx, server)
}

func runUsageCases(t *testing.T, cases []usageCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := serveCapturing(t, tc.response)

			got, err := tc.call(t.Context(), server)
			if err != nil {
				t.Fatalf("call failed: %v", err)
			}

			if got != tc.want {
				t.Errorf("usage = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestChat_ReportsProviderUsage(t *testing.T) {
	runUsageCases(t, []usageCase{
		{
			name:     "claude reported",
			response: `{"type":"message","role":"assistant","content":[{"type":"text","text":"{}"}],"usage":{"input_tokens":120,"output_tokens":30}}`,
			call:     claudeChat,
			want:     Usage{InputTokens: 120, OutputTokens: 30, Reported: true},
		},
		{
			name:     "claude without usage",
			response: `{"type":"message","role":"assistant","content":[{"type":"text","text":"{}"}]}`,
			call:     claudeChat,
		},
		{
			name:     "openai reported",
			response: `{"choices":[{"message":{"content":"{}"}}],"usage":{"prompt_tokens":200,"completion_tokens":40,"total_tokens":240}}`,
			call:     openAIChat,
			want:     Usage{InputTokens: 200, OutputTokens: 40, Reported: true},
		},
		{
			name:     "openai-compatible server without usage",
			response: `{"choices":[{"message":{"content":"{}"}}]}`,
			call:     openAIChat,
		},
		{
			name:     "gemini counts thinking tokens as output",
			response: `{"candidates":[{"content":{"parts":[{"text":"{}"}]}}],"usageMetadata":{"promptTokenCount":300,"candidatesTokenCount":20,"thoughtsTokenCount":50}}`,
			call:     geminiChat,
			want:     Usage{InputTokens: 300, OutputTokens: 70, Reported: true},
		},
		{
			name:     "gemini without usage metadata",
			response: `{"candidates":[{"content":{"parts":[{"text":"{}"}]}}]}`,
			call:     geminiChat,
		},
		{
			name:     "ollama reported on the final response",
			response: `{"model":"llama3.2","message":{"role":"assistant","content":"{}"},"done":true,"prompt_eval_count":90,"eval_count":12}`,
			call:     ollamaChat,
			want:     Usage{InputTokens: 90, OutputTokens: 12, Reported: true},
		},
		{
			name:     "ollama response that never finished",
			response: `{"model":"llama3.2","message":{"role":"assistant","content":"{}"},"done":false}`,
			call:     ollamaChat,
		},
	})
}

func embeddingUsage(embed Embedder) func(ctx context.Context, _ *httptest.Server) (Usage, error) {
	return func(ctx context.Context, _ *httptest.Server) (Usage, error) {
		embedding, err := embed.CreateEmbedding(ctx, "text", EmbeddingTaskQuery)
		return embedding.Usage, err
	}
}

func openAIEmbed(ctx context.Context, server *httptest.Server) (Usage, error) {
	return embeddingUsage(NewOpenAIProviderWithBaseURL("key", "", "text-embedding-3-small", server.URL, server.Client()))(ctx, server)
}

func voyageEmbed(model string) func(ctx context.Context, server *httptest.Server) (Usage, error) {
	return func(ctx context.Context, server *httptest.Server) (Usage, error) {
		return embeddingUsage(NewVoyageProviderWithBaseURL("key", model, server.URL, server.Client()))(ctx, server)
	}
}

func geminiEmbed(ctx context.Context, server *httptest.Server) (Usage, error) {
	return embeddingUsage(&GeminiProvider{apiKey: "key", embedModel: "gemini-embedding-001", baseURL: server.URL, client: server.Client()})(ctx, server)
}

func ollamaEmbed(ctx context.Context, server *httptest.Server) (Usage, error) {
	return embeddingUsage(NewOllamaProviderWithBaseURL(server.URL, "", "nomic-embed-text"))(ctx, server)
}

func TestCreateEmbedding_ReportsProviderUsage(t *testing.T) {
	runUsageCases(t, []usageCase{
		{
			name:     "openai reported",
			response: `{"data":[{"embedding":[0.1]}],"usage":{"prompt_tokens":8,"total_tokens":8}}`,
			call:     openAIEmbed,
			want:     Usage{InputTokens: 8, Reported: true},
		},
		{
			name:     "openai-compatible server without usage",
			response: `{"data":[{"embedding":[0.1]}]}`,
			call:     openAIEmbed,
		},
		{
			name:     "voyage reported",
			response: `{"data":[{"embedding":[0.1]}],"usage":{"total_tokens":6}}`,
			call:     voyageEmbed("voyage-4"),
			want:     Usage{InputTokens: 6, Reported: true},
		},
		{
			name:     "voyage contextualized reported",
			response: `{"data":[{"data":[{"embedding":[0.1]}]}],"usage":{"total_tokens":7}}`,
			call:     voyageEmbed("voyage-context-3"),
			want:     Usage{InputTokens: 7, Reported: true},
		},
		{
			name:     "voyage without usage",
			response: `{"data":[{"embedding":[0.1]}]}`,
			call:     voyageEmbed("voyage-4"),
		},
		{
			name:     "gemini never reports",
			response: `{"embeddings":[{"values":[0.1]}]}`,
			call:     geminiEmbed,
		},
		{
			name:     "ollama never reports",
			response: `{"embedding":[0.1]}`,
			call:     ollamaEmbed,
		},
	})
}

func TestMeteredChatter_TotalsConcurrentCalls(t *testing.T) {
	meter := NewMeteredChatter(&MockProvider{ChatUsage: Usage{InputTokens: 10, OutputTokens: 2, Reported: true}})

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := meter.Chat(t.Context(), "system", "user"); err != nil {
				t.Errorf("Chat failed: %v", err)
			}
		})
	}

	wg.Wait()

	want := Usage{InputTokens: 200, OutputTokens: 40, Reported: true}
	if got := meter.Usage(); got != want {
		t.Errorf("Usage() = %+v, want %+v", got, want)
	}

	if got := meter.Calls(); got != 20 {
		t.Errorf("Calls() = %d, want 20", got)
	}
}

func TestMeteredChatter_UsageTotals(t *testing.T) {
	reported := Usage{InputTokens: 10, OutputTokens: 2, Reported: true}

	tests := []struct {
		name  string
		calls []Usage
		want  Usage
	}{
		{"no calls is a reported zero", nil, Usage{Reported: true}},
		{"every call reported", []Usage{reported, reported}, Usage{InputTokens: 20, OutputTokens: 4, Reported: true}},
		{"one unreported call makes the total unreported", []Usage{reported, {}}, Usage{InputTokens: 10, OutputTokens: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockProvider{}
			meter := NewMeteredChatter(mock)

			for _, usage := range tt.calls {
				mock.ChatUsage = usage

				if _, err := meter.Chat(t.Context(), "system", "user"); err != nil {
					t.Fatalf("Chat failed: %v", err)
				}
			}

			if got := meter.Usage(); got != tt.want {
				t.Errorf("Usage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMeteredChatter_FailedCallCountsAsACallWithNoTokens(t *testing.T) {
	mock := &MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) { return "", errors.New("down") },
	}
	meter := NewMeteredChatter(mock)

	if _, err := meter.Chat(t.Context(), "system", "user"); err == nil {
		t.Fatal("expected the provider's error")
	}

	if got, want := meter.Usage(), (Usage{Reported: true}); got != want {
		t.Errorf("Usage() = %+v, want %+v", got, want)
	}

	if got := meter.Calls(); got != 1 {
		t.Errorf("Calls() = %d, want 1", got)
	}
}

func TestMeteredEmbedder_TotalsCalls(t *testing.T) {
	meter := NewMeteredEmbedder(&MockProvider{EmbeddingDim: 2, EmbedUsage: Usage{InputTokens: 4, Reported: true}})

	for range 3 {
		if _, err := meter.CreateEmbedding(t.Context(), "text", EmbeddingTaskQuery); err != nil {
			t.Fatalf("CreateEmbedding failed: %v", err)
		}
	}

	if got, want := meter.Usage(), (Usage{InputTokens: 12, Reported: true}); got != want {
		t.Errorf("Usage() = %+v, want %+v", got, want)
	}

	if got := meter.Calls(); got != 3 {
		t.Errorf("Calls() = %d, want 3", got)
	}
}
