package cli

import (
	"fmt"
	"os"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

// apiKey is embedEnvKey whenever reuse is false, never chatAPIKey.
func resolveEmbedProvider(cfg *config.Config, chatAPIKey, embedEnvKey string) (name, apiKey string, reuse bool) {
	name = cfg.VectorStore.Provider
	if name == "" {
		name = cfg.LLM.Provider
	}

	if name == cfg.LLM.Provider {
		return name, chatAPIKey, true
	}

	return name, embedEnvKey, false
}

// Mock-injection counterpart of resolveEmbedProvider; errors rather than silently reusing chatProvider.
func resolveEmbedProviderInstance(cfg *config.Config, chatProvider inference.Embedder, embedFactory func(*config.Config) inference.Embedder) (inference.Embedder, error) {
	_, _, reuse := resolveEmbedProvider(cfg, "", "")
	switch {
	case reuse:
		return chatProvider, nil
	case embedFactory != nil:
		return embedFactory(cfg), nil
	default:
		return nil, fmt.Errorf("ProviderFactories.Embed is required: llm.provider and vector_store.provider name different providers")
	}
}

func selectProviders(warnings *output.Printer, cfg *config.Config, factories ProviderFactories) (inference.Chatter, inference.Embedder, error) {
	if factories.Chat == nil {
		return buildProviders(warnings, cfg, os.Getenv("ARCHGUARD_API_KEY"), os.Getenv("ARCHGUARD_EMBEDDING_API_KEY"))
	}

	chat := factories.Chat(cfg)

	embed, err := resolveEmbedProviderInstance(cfg, chat, factories.Embed)
	if err != nil {
		return nil, nil, err
	}

	return chat, embed, nil
}

func buildProviders(warnings *output.Printer, cfg *config.Config, chatAPIKey, embedEnvKey string) (inference.Chatter, inference.Embedder, error) {
	embedName, embedAPIKey, reuse := resolveEmbedProvider(cfg, chatAPIKey, embedEnvKey)
	if reuse {
		provider, err := buildProvider(warnings, cfg.LLM.Provider, chatAPIKey, cfg)
		if err != nil {
			return nil, nil, err
		}

		return provider, provider, nil
	}

	chat, err := buildChatProvider(warnings, cfg.LLM.Provider, chatAPIKey, cfg)
	if err != nil {
		return nil, nil, err
	}

	embed, err := buildEmbedProvider(warnings, embedName, embedAPIKey, cfg)
	if err != nil {
		return nil, nil, err
	}

	return chat, embed, nil
}

func buildProvider(warnings *output.Printer, name, apiKey string, cfg *config.Config) (inference.Provider, error) {
	switch name {
	case "openai":
		if apiKey == "" {
			warnings.Warn("no API key set for %s provider. Requests may fail.", name)
		}

		return inference.NewOpenAIProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model), nil
	case "ollama":
		return inference.NewOllamaProvider(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.VectorStore.Model, cfg.LLM.Temperature), nil
	case "gemini":
		if apiKey == "" {
			warnings.Warn("no API key set for %s provider. Requests may fail.", name)
		}

		return inference.NewGeminiProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

func buildChatProvider(warnings *output.Printer, name, apiKey string, cfg *config.Config) (inference.Chatter, error) {
	if name != "claude" {
		return buildProvider(warnings, name, apiKey, cfg)
	}

	if apiKey == "" {
		warnings.Warn("no API key set for %s provider. Requests may fail.", name)
	}

	return inference.NewClaudeProvider(apiKey, cfg.LLM.Model), nil
}

func buildEmbedProvider(warnings *output.Printer, name, apiKey string, cfg *config.Config) (inference.Embedder, error) {
	if name != "voyage" {
		return buildProvider(warnings, name, apiKey, cfg)
	}

	if apiKey == "" {
		warnings.Warn("no API key set for %s provider. Requests may fail.", name)
	}

	return inference.NewVoyageProvider(apiKey, cfg.VectorStore.Model), nil
}
