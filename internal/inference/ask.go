package inference

import (
	"context"
	"errors"
)

// Asker answers named yes/no questions about one text in a single call, with each answer's probability of yes.
type Asker interface {
	Ask(ctx context.Context, state string, questions map[string]string) (map[string]float64, error)
}

// An Asker's error matches these with errors.Is, so callers never depend on which model answered.
var (
	ErrKeyRejected   = errors.New("api key rejected")
	ErrInputTooLarge = errors.New("input too large")
)
