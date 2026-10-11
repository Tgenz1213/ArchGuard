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
	calls      int
	sum        Usage
	unreported bool
}

// A failed call is still a call, but it adds no tokens: the backend reported no usage for it.
func (t *usageTotal) record(usage Usage, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.calls++

	if err != nil {
		return
	}

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

func (t *usageTotal) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.calls
}

type MeteredChatter struct {
	Chatter
	usage usageTotal
}

func NewMeteredChatter(chat Chatter) *MeteredChatter {
	return &MeteredChatter{Chatter: chat}
}

func (m *MeteredChatter) Chat(ctx context.Context, systemPrompt, userPrompt string) (ChatReply, error) {
	reply, err := m.Chatter.Chat(ctx, systemPrompt, userPrompt)
	m.usage.record(reply.Usage, err)

	return reply, err
}

func (m *MeteredChatter) Usage() Usage {
	return m.usage.total()
}

func (m *MeteredChatter) Calls() int {
	return m.usage.callCount()
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
	m.usage.record(embedding.Usage, err)

	return embedding, err
}

func (m *MeteredEmbedder) Usage() Usage {
	return m.usage.total()
}

func (m *MeteredEmbedder) Calls() int {
	return m.usage.callCount()
}
