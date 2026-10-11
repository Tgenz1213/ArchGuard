package inference

import (
	"context"
	"sync"
)

// Reported is false when the backend sent no usage, so a missing figure is never read as zero.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Reported     bool
}

type ChatReply struct {
	Text  string
	Usage Usage
}

// An embedding call has no output, so only Usage.InputTokens is set.
type Embedding struct {
	Vector []float32
	Usage  Usage
}

type usageTotal struct {
	mu         sync.Mutex
	sum        Usage
	unreported bool
}

func (t *usageTotal) add(usage Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sum.InputTokens += usage.InputTokens
	t.sum.OutputTokens += usage.OutputTokens
	t.unreported = t.unreported || !usage.Reported
}

// One call without usage makes the whole total unreported, since a partial sum would undercount.
func (t *usageTotal) total() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()

	return Usage{InputTokens: t.sum.InputTokens, OutputTokens: t.sum.OutputTokens, Reported: !t.unreported}
}

// A failed call adds nothing: the backend reported no usage for it.
type MeteredChatter struct {
	Chatter
	usage usageTotal
}

func NewMeteredChatter(chat Chatter) *MeteredChatter {
	return &MeteredChatter{Chatter: chat}
}

func (m *MeteredChatter) Chat(ctx context.Context, systemPrompt, userPrompt string) (ChatReply, error) {
	reply, err := m.Chatter.Chat(ctx, systemPrompt, userPrompt)
	if err != nil {
		return reply, err
	}

	m.usage.add(reply.Usage)

	return reply, nil
}

func (m *MeteredChatter) Usage() Usage {
	return m.usage.total()
}

type MeteredEmbedder struct {
	Embedder
	usage usageTotal
}

func NewMeteredEmbedder(embed Embedder) *MeteredEmbedder {
	return &MeteredEmbedder{Embedder: embed}
}

func (m *MeteredEmbedder) CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) (Embedding, error) {
	embedding, err := m.Embedder.CreateEmbedding(ctx, text, task)
	if err != nil {
		return embedding, err
	}

	m.usage.add(embedding.Usage)

	return embedding, nil
}

func (m *MeteredEmbedder) Usage() Usage {
	return m.usage.total()
}
