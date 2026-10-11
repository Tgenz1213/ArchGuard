package telemetry

import (
	"context"

	"github.com/tgenz1213/archguard/internal/inference"
)

type Chatter struct {
	inference.Chatter
	role     Role
	recorder Recorder
}

func NewChatter(chat inference.Chatter, role Role, recorder Recorder) *Chatter {
	return &Chatter{Chatter: chat, role: role, recorder: recorder}
}

func (c *Chatter) Chat(ctx context.Context, systemPrompt, userPrompt string) (inference.ChatReply, error) {
	reply, err := c.Chatter.Chat(ctx, systemPrompt, userPrompt)
	c.recorder.Record(RequestSent{Role: c.role, Usage: reply.Usage, Err: err})

	return reply, err
}

type Embedder struct {
	inference.Embedder
	recorder Recorder
}

func NewEmbedder(embed inference.Embedder, recorder Recorder) *Embedder {
	return &Embedder{Embedder: embed, recorder: recorder}
}

func (e *Embedder) CreateEmbedding(ctx context.Context, text string, task inference.EmbeddingTaskType) (inference.Embedding, error) {
	embedding, err := e.Embedder.CreateEmbedding(ctx, text, task)
	e.recorder.Record(RequestSent{Role: Embedding, Usage: embedding.Usage, Err: err})

	return embedding, err
}
