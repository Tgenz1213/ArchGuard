package stage

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
	QueryText() string
}

// Scorers only score (one value per candidate, in candidate order, on the scale of their stage's Threshold); dropping is a Stage's job.
type Scorer interface {
	Score(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]float64, error)
}
