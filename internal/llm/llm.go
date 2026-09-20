package llm

import "context"

type Provider interface {
	// Providers without an asymmetric-retrieval mechanism may ignore task.
	CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error)
	Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// CountTokens uses each provider's own tokenizer, not a shared one.
	CountTokens(ctx context.Context, text string) (int, error)
}
