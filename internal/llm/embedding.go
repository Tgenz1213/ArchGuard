package llm

import "context"

// Embedder is the embed-only slice of Provider, so embedding consumers needn't depend on chat.
type Embedder interface {
	// Providers without an asymmetric-retrieval mechanism may ignore task.
	CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error)
}

// A document is ADR content being indexed; a query is a diff or code being searched.
type EmbeddingTaskType int

const (
	EmbeddingTaskDocument EmbeddingTaskType = iota
	EmbeddingTaskQuery
)

// Pick lets providers map the task role onto their backend's convention in one line.
func (t EmbeddingTaskType) Pick(document, query string) string {
	if t == EmbeddingTaskQuery {
		return query
	}
	return document
}
