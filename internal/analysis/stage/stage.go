package stage

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type Kind int

const (
	KindUnavailable Kind = iota
	KindPreconditionNotMet
)

func (k Kind) String() string {
	if k == KindPreconditionNotMet {
		return "precondition_not_met"
	}
	return "unavailable"
}

func (k Kind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// Error names the action that failed so the engine can report it without knowing which scorer ran.
type Error struct {
	Action string
	Kind   Kind
	Err    error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

type Stage struct {
	Name        string
	Scorer      Scorer
	Min         Threshold
	MaxKeep     int
	FailOnError bool
}

func (s Stage) Apply(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]Candidate, error) {
	scores, err := s.Scorer.Score(ctx, file, debug, candidates)
	if err != nil {
		var stageErr *Error
		if errors.As(err, &stageErr) {
			return nil, err
		}
		return nil, &Error{Action: "scoring candidates", Err: err}
	}
	if len(scores) != len(candidates) {
		return nil, &Error{Action: "scoring candidates", Err: fmt.Errorf("scorer returned %d scores for %d candidates", len(scores), len(candidates))}
	}

	floor := s.Min
	if floor == nil {
		floor = FixedMin(0)
	}

	var qualifying, below []Candidate
	for i, c := range candidates {
		c.Score = scores[i]
		if c.Score >= floor.For(c.ADR) {
			qualifying = append(qualifying, c)
		} else {
			below = append(below, c)
		}
	}
	sort.SliceStable(qualifying, func(i, j int) bool { return qualifying[i].Score > qualifying[j].Score })

	kept := qualifying
	if s.MaxKeep > 0 && len(kept) > s.MaxKeep {
		kept = kept[:s.MaxKeep]
	}

	if debug.Enabled() {
		debug.Printf("  Stage %s: %d candidate(s) received, %d kept\n", s.Name, len(candidates), len(kept))
		for _, c := range kept {
			debug.Printf("  Kept: %s (score %.2f)\n", c.ADR.Title, c.Score)
		}
		sort.SliceStable(below, func(i, j int) bool { return below[i].Score > below[j].Score })
		for _, c := range below {
			debug.Printf("  Below threshold: %s (score %.2f < threshold %.2f)\n", c.ADR.Title, c.Score, floor.For(c.ADR))
		}
		for i, c := range qualifying[len(kept):] {
			debug.Printf("  Cut by top-K limit: %s (score %.2f, rank %d of %d qualifying ADRs)\n", c.ADR.Title, c.Score, len(kept)+i+1, len(qualifying))
		}
	}

	return kept, nil
}
