package inference

import (
	"context"
)

type MockProvider struct {
	EmbedFunc       func(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error)
	ChatFunc        func(ctx context.Context, system, user string) (string, error)
	CountTokensFunc func(ctx context.Context, text string) (int, error)
	Debug           bool
	EmbeddingDim    int
	// Attached to every successful call; the zero value reports no usage.
	ChatUsage  Usage
	EmbedUsage Usage
}

func (m *MockProvider) SetDebug(debug bool) {
	m.Debug = debug
}

func (m *MockProvider) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) (Embedding, error) {
	if m.EmbedFunc == nil {
		return Embedding{Vector: m.defaultEmbedding(), Usage: m.EmbedUsage}, nil
	}

	vector, err := m.EmbedFunc(ctx, text, task)
	if err != nil {
		return Embedding{}, err
	}

	return Embedding{Vector: vector, Usage: m.EmbedUsage}, nil
}

func (m *MockProvider) defaultEmbedding() []float32 {
	// Return a non-zero vector to avoid NaN in cosine similarity (0/0)
	dim := m.EmbeddingDim
	if dim == 0 {
		dim = 1536
	}

	v := make([]float32, dim)
	v[0] = 1.0
	return v
}

func (m *MockProvider) Chat(ctx context.Context, system, user string) (ChatReply, error) {
	if m.ChatFunc == nil {
		return ChatReply{Text: `{"violation": false, "reasoning": "default mock", "quoted_code": ""}`, Usage: m.ChatUsage}, nil
	}

	text, err := m.ChatFunc(ctx, system, user)
	if err != nil {
		return ChatReply{}, err
	}

	return ChatReply{Text: text, Usage: m.ChatUsage}, nil
}

func (m *MockProvider) CountTokens(ctx context.Context, text string) (int, error) {
	if m.CountTokensFunc != nil {
		return m.CountTokensFunc(ctx, text)
	}

	return len(text) / 4, nil
}
