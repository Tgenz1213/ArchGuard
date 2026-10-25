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

// decision is what a stage did with one file's candidates, kept as data so the debug report is a separate step.
type decision struct {
	received   int
	kept       []Candidate
	below      []Candidate
	qualifying []Candidate
	floor      Threshold
}

func (s Stage) Apply(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]Candidate, error) {
	decided, err := s.decide(ctx, file, debug, candidates)
	if err != nil {
		return nil, err
	}

	if debug.DebugEnabled() {
		s.report(debug, decided)
	}

	return decided.kept, nil
}

func (s Stage) decide(ctx context.Context, file File, debug Debug, candidates []Candidate) (decision, error) {
	if len(candidates) == 0 {
		return decision{}, nil
	}

	scores, err := s.Scorer.Score(ctx, file, debug, candidates)
	if err != nil {
		var stageErr *Error
		if errors.As(err, &stageErr) {
			return decision{}, err
		}

		return decision{}, &Error{Action: "scoring candidates", Err: err}
	}

	if len(scores) != len(candidates) {
		return decision{}, &Error{
			Action: "scoring candidates",
			Err: fmt.Errorf(
				"scorer returned %d scores for %d candidates",
				len(scores),
				len(candidates),
			),
		}
	}

	floor := s.Min
	if floor == nil {
		floor = FixedMin(0)
	}

	var qualifying, below []Candidate
	for i, candidate := range candidates {
		candidate.Score = scores[i]
		if candidate.Score >= floor.For(candidate.ADR) {
			qualifying = append(qualifying, candidate)
		} else {
			below = append(below, candidate)
		}
	}

	sort.SliceStable(qualifying, func(i, j int) bool { return qualifying[i].Score > qualifying[j].Score })
	sort.SliceStable(below, func(i, j int) bool { return below[i].Score > below[j].Score })

	kept := qualifying
	if s.MaxKeep > 0 && len(kept) > s.MaxKeep {
		kept = kept[:s.MaxKeep]
	}

	return decision{
		received:   len(candidates),
		kept:       kept,
		below:      below,
		qualifying: qualifying,
		floor:      floor,
	}, nil
}

func (d decision) cut() []Candidate { return d.qualifying[len(d.kept):] }

func (s Stage) report(debug Debug, d decision) {
	debug.Debug("Stage %s: %d candidate(s) received, %d kept", s.Name, d.received, len(d.kept))
	for _, candidate := range d.kept {
		debug.Debug("Kept: %s (score %.2f)", candidate.ADR.Title, candidate.Score)
	}

	for _, candidate := range d.below {
		debug.Debug(
			"Below threshold: %s (score %.2f < threshold %.2f)",
			candidate.ADR.Title,
			candidate.Score,
			d.floor.For(candidate.ADR),
		)
	}

	for i, candidate := range d.cut() {
		debug.Debug(
			"Cut by top-K limit: %s (score %.2f, rank %d of %d qualifying ADRs)",
			candidate.ADR.Title,
			candidate.Score,
			len(d.kept)+i+1,
			len(d.qualifying),
		)
	}
}
