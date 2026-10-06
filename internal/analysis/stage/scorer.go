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
	QueryText(ctx context.Context) string
}

// A Scorer only scores, one value per candidate in order; dropping is a Stage's job.
type Scorer interface {
	Score(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]float64, error)
}
