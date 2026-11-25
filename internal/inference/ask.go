package inference

import (
	"context"
	"errors"
)

// Asker answers named yes/no questions about one text in a single call, with each answer's probability of yes and the usage the server reported.
type Asker interface {
	Ask(ctx context.Context, state string, questions map[string]string) (AskReply, error)
}

// Retries counts the requests sent after the first, and is set when Ask fails too.
type AskReply struct {
	Answers map[string]float64
	Usage   Usage
	Retries int
}

// An Asker's error matches these with errors.Is, so callers never depend on which model answered. A rejected key stops the whole check, since no retry or pass-through fixes it.
var (
	ErrKeyRejected   = errors.New("api key rejected")
	ErrInputTooLarge = errors.New("input too large")
)
