package telemetry

import (
	"context"
	"errors"

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
	embedder inference.Embedder
	recorder Recorder
}

func NewEmbedder(embed inference.Embedder, recorder Recorder) *Embedder {
	return &Embedder{embedder: embed, recorder: recorder}
}

func (e *Embedder) CreateEmbedding(ctx context.Context, text string, task inference.EmbeddingTaskType) (inference.Embedding, error) {
	embedding, err := e.embedder.CreateEmbedding(ctx, text, task)
	e.recorder.Record(RequestSent{Role: Embedding, Usage: embedding.Usage, Err: err})

	return embedding, err
}

var errRetried = errors.New("retried")

type Asker struct {
	asker    inference.Asker
	recorder Recorder
}

func NewAsker(asker inference.Asker, recorder Recorder) *Asker {
	return &Asker{asker: asker, recorder: recorder}
}

// Ask records each retry the asker sent as a failed request, so calls count every request, as they do for the other roles.
func (a *Asker) Ask(ctx context.Context, state string, questions map[string]string) (inference.AskReply, error) {
	reply, err := a.asker.Ask(ctx, state, questions)

	for range reply.Retries {
		a.recorder.Record(RequestSent{Role: Asking, Err: errRetried})
	}

	if reply.Retries > 0 {
		a.recorder.Record(RetriesSent{Role: Asking, Retries: reply.Retries})
	}

	a.recorder.Record(RequestSent{Role: Asking, Usage: reply.Usage, Err: err})

	return reply, err
}
