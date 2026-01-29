package llm

import (
	"context"
)

type MockProvider struct {
	EmbedFunc func(ctx context.Context, text string) ([]float32, error)
	// ChatFunc allows you to mock the raw string response from an LLM
	ChatFunc     func(ctx context.Context, system, user string) (string, error)
	Debug        bool
	EmbeddingDim int
}

func (m *MockProvider) SetDebug(debug bool) {
	m.Debug = debug
}

func (m *MockProvider) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, text)
	}
	// Return a non-zero vector to avoid NaN in cosine similarity (0/0)
	dim := m.EmbeddingDim
	if dim == 0 {
		dim = 1536
	}
	v := make([]float32, dim)
	v[0] = 1.0
	return v, nil
}

func (m *MockProvider) Chat(ctx context.Context, system, user string) (string, error) {
	if m.ChatFunc != nil {
		return m.ChatFunc(ctx, system, user)
	}
	// Default mock response as a JSON string
	return `{"violation": false, "reasoning": "default mock", "quoted_code": ""}`, nil
}
