package node

import (
	"context"

	"github.com/tgenz1213/archguard/internal/index"
)

type Candidate struct {
	ADR   *index.ADR
	Score float64
}

type File interface {
	Path() string
	QueryText(ctx context.Context) string
	// Sides is what a runner that answers without a quote reads: the content the LLM judges, or each side of it when that is a diff.
	Sides() []string
}

// A Scorer only scores, one value per candidate in order; dropping is a Node's job.
// +Inf marks a candidate it does not judge, which the Node keeps with its earlier score.
type Scorer interface {
	Score(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]float64, error)
}
