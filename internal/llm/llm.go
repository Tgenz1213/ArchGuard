package llm

import "context"

// Chatter includes CountTokens because it sizes chat prompts against llm.max_tokens.
type Chatter interface {
	Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// CountTokens uses each provider's own tokenizer, not a shared one.
	CountTokens(ctx context.Context, text string) (int, error)
}

type Provider interface {
	Embedder
	Chatter
}
