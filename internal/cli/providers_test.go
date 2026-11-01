package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestResolveEmbedProvider_SameProviderReusesInstance(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "openai"},
		VectorStore: config.VectorStore{Provider: ""},
	}

	name, _, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if !reuse {
		t.Error("expected reuse=true when vector_store.provider is unset")
	}

	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
}

func TestResolveEmbedProvider_ExplicitSameProviderReusesInstance(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "openai"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}

	_, _, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if !reuse {
		t.Error("expected reuse=true when vector_store.provider explicitly matches llm.provider")
	}
}

func TestResolveEmbedProvider_DifferentProviderUsesEmbedKey(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}

	name, apiKey, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if reuse {
		t.Error("expected reuse=false for different providers")
	}

	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}

	if apiKey != "embed-key" {
		t.Errorf("expected embed-key, got %q", apiKey)
	}
}

func TestResolveEmbedProvider_DifferentProviderNeverFallsBackToChatKey(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}

	name, apiKey, reuse := resolveEmbedProvider(cfg, "chat-key", "")
	if reuse {
		t.Error("expected reuse=false for different providers")
	}

	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}

	if apiKey == "chat-key" {
		t.Fatal("REGRESSION: embed provider fell back to the chat provider's API key -- this is the exact credential-leak bug fixed in fee5a7c")
	}

	if apiKey != "" {
		t.Errorf("expected empty apiKey (embed key was unset, must not substitute chat key), got %q", apiKey)
	}
}

func TestResolveEmbedProviderInstance_ReusesChatProviderWhenNamesMatch(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "openai"}}
	chat := &inference.MockProvider{}

	got, err := resolveEmbedProviderInstance(cfg, chat, func(*config.Config) inference.Embedder {
		t.Error("the embed factory must not be called when the provider names match")
		return &inference.MockProvider{}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != inference.Embedder(chat) {
		t.Error("expected the chat provider instance to be reused")
	}
}

func TestResolveEmbedProviderInstance_BuildsFromFactoryWhenNamesDiffer(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	chat := &inference.MockProvider{}
	embed := &inference.MockProvider{}

	got, err := resolveEmbedProviderInstance(cfg, chat, func(*config.Config) inference.Embedder { return embed })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != inference.Embedder(embed) {
		t.Error("expected the embed factory's provider to be used, not the chat provider")
	}
}

func TestResolveEmbedProviderInstance_ErrorsWhenEmbedFactoryRequiredButNil(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	chat := &inference.MockProvider{}

	if _, err := resolveEmbedProviderInstance(cfg, chat, nil); err == nil {
		t.Fatal("expected an error when the roles need different providers but embedFactory is nil")
	}
}

func TestBuildRoleProviders_ClaudeAndVoyage(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Model: "claude-sonnet-4-5"},
		VectorStore: config.VectorStore{Model: "voyage-4"},
	}

	claude, err := buildChatProvider(output.Discard(), "claude", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildChatProvider(claude) failed: %v", err)
	}

	if _, ok := claude.(*inference.ClaudeProvider); !ok {
		t.Errorf("expected *inference.ClaudeProvider, got %T", claude)
	}

	voyage, err := buildEmbedProvider(output.Discard(), "voyage", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildEmbedProvider(voyage) failed: %v", err)
	}

	if _, ok := voyage.(*inference.VoyageProvider); !ok {
		t.Errorf("expected *inference.VoyageProvider, got %T", voyage)
	}
}

// Under --format json, stdout must carry only the JSON document.
func TestBuildProvider_MissingAPIKeyWarningRespectsWriter(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Model: "gpt-4"}}

	tests := []struct {
		name  string
		build func(*output.Printer) error
	}{
		{"openai", func(p *output.Printer) error { _, err := buildProvider(p, "openai", "", cfg); return err }},
		{"claude", func(p *output.Printer) error { _, err := buildChatProvider(p, "claude", "", cfg); return err }},
		{"voyage", func(p *output.Printer) error { _, err := buildEmbedProvider(p, "voyage", "", cfg); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tt.build(output.New(&buf, false)); err != nil {
				t.Fatalf("build failed: %v", err)
			}

			if !strings.Contains(buf.String(), "no API key set") {
				t.Errorf("expected the missing-API-key warning on the given writer, got: %q", buf.String())
			}
		})
	}
}

func TestBuildProviders_SameNameSharesOneInstance(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "openai", Model: "gpt-4"}}

	var buf bytes.Buffer

	chat, embed, err := buildProviders(output.New(&buf, false), cfg, "", "")
	if err != nil {
		t.Fatalf("buildProviders failed: %v", err)
	}

	if any(chat) != any(embed) {
		t.Error("expected one instance to serve both roles")
	}

	if n := strings.Count(buf.String(), "no API key set"); n != 1 {
		t.Errorf("expected one missing-API-key warning, got %d: %q", n, buf.String())
	}
}

func TestBuildProviders_OllamaChatSendsConfiguredTemperature(t *testing.T) {
	var options map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Options map[string]any `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request body: %v", err)
		}

		options = body.Options

		if _, err := w.Write([]byte(`{"message":{"role":"assistant","content":"{}"},"done":true}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &config.Config{LLM: config.LLMConfig{Provider: "ollama", Model: "m", BaseURL: server.URL, Temperature: new(0.4)}}

	chat, _, err := buildProviders(output.Discard(), cfg, "", "")
	if err != nil {
		t.Fatalf("buildProviders failed: %v", err)
	}

	if _, err := chat.Chat(t.Context(), "system", "user"); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if got := options["temperature"]; got != 0.4 {
		t.Errorf("temperature = %v, want 0.4", got)
	}
}

func TestBuildProviders_EveryChatProviderGetsConfiguredTemperature(t *testing.T) {
	type temperatureReader interface{ Temperature() *float64 }

	for _, name := range []string{"openai", "gemini", "ollama", "claude"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{
				LLM:         config.LLMConfig{Provider: name, Model: "m", Temperature: new(0.3)},
				VectorStore: config.VectorStore{Provider: "ollama", Model: "e"},
			}

			chat, _, err := buildProviders(output.Discard(), cfg, "key", "")
			if err != nil {
				t.Fatalf("buildProviders failed: %v", err)
			}

			reader, ok := chat.(temperatureReader)
			if !ok {
				t.Fatalf("%T does not expose its temperature", chat)
			}

			if got := reader.Temperature(); got == nil || *got != 0.3 {
				t.Errorf("temperature = %v, want 0.3", got)
			}
		})
	}
}

func TestBuildProviders_DifferentNamesBuildEachRole(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-5"},
		VectorStore: config.VectorStore{Provider: "voyage", Model: "voyage-4"},
	}

	var buf bytes.Buffer

	chat, embed, err := buildProviders(output.New(&buf, false), cfg, "chat-key", "")
	if err != nil {
		t.Fatalf("buildProviders failed: %v", err)
	}

	if _, ok := chat.(*inference.ClaudeProvider); !ok {
		t.Errorf("expected chat to be *inference.ClaudeProvider, got %T", chat)
	}

	if _, ok := embed.(*inference.VoyageProvider); !ok {
		t.Errorf("expected embed to be *inference.VoyageProvider, got %T", embed)
	}

	if n := strings.Count(buf.String(), "no API key set"); n != 1 {
		t.Errorf("expected only the embedding provider's missing-API-key warning, got %d: %q", n, buf.String())
	}
}

func TestBuildProviders_UnknownNameFails(t *testing.T) {
	for _, cfg := range []*config.Config{
		{LLM: config.LLMConfig{Provider: "nope"}},
		{LLM: config.LLMConfig{Provider: "claude"}, VectorStore: config.VectorStore{Provider: "nope"}},
	} {
		if _, _, err := buildProviders(output.Discard(), cfg, "k", "k"); err == nil {
			t.Errorf("expected an error for llm.provider %q, vector_store.provider %q", cfg.LLM.Provider, cfg.VectorStore.Provider)
		}
	}
}

func TestNewScreeners(t *testing.T) {
	rank := config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine}
	ask := config.Node{Name: "screen", Job: config.JobAsk, With: config.RunnerJev, Model: "jev-2"}
	injected := func(string) stage.Screener { return nil }

	tests := []struct {
		name         string
		nodes        []config.Node
		key          string
		injected     func(string) stage.Screener
		wantFactory  bool
		wantErrNames string
	}{
		{name: "no jev node needs no key", nodes: []config.Node{rank}},
		{name: "jev node without a key", nodes: []config.Node{rank, ask}, wantErrNames: `"screen"`},
		{name: "jev node without a key even when injected", nodes: []config.Node{rank, ask}, injected: injected, wantErrNames: `"screen"`},
		{name: "jev node with a key", nodes: []config.Node{rank, ask}, key: "k", wantFactory: true},
		{name: "jev node with a key and an injected screener", nodes: []config.Node{rank, ask}, key: "k", injected: injected, wantFactory: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.JevAPIKeyEnv, tt.key)

			factory, err := newScreeners(tt.nodes, tt.injected)
			if tt.wantErrNames != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrNames) || !strings.Contains(err.Error(), config.JevAPIKeyEnv) {
					t.Fatalf("err = %v, want one naming %s and %s", err, tt.wantErrNames, config.JevAPIKeyEnv)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if (factory != nil) != tt.wantFactory {
				t.Errorf("factory present = %v, want %v", factory != nil, tt.wantFactory)
			}
		})
	}
}
