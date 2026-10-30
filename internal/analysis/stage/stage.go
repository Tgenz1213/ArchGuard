package stage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
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
	// Unrecoverable is set for a failure, such as a rejected API key, that on_error: skip must not hide.
	Unrecoverable bool
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

type Stage struct {
	Name         string
	Scorer       Scorer
	Min          Threshold
	MaxKeep      int
	FailOnError  bool
	ListRejected bool
	// Under on_error: skip, the stage passes its candidates on instead of the file being skipped.
	PassThroughOnSkip bool
	// Dropping every candidate means the stage found nothing worth judging, not that none were relevant.
	Screens bool
}

// Skipped is a stage error the stage recovered from by passing its candidates on unchanged.
type Skipped struct{ Err error }

func (e *Skipped) Error() string { return e.Err.Error() }
func (e *Skipped) Unwrap() error { return e.Err }

// decision is what a stage did with one file's candidates, kept as data so the debug report is a separate step.
type decision struct {
	received   int
	kept       []Candidate
	passed     []Candidate
	below      []Candidate
	qualifying []Candidate
	floor      Threshold
}

func (s Stage) Apply(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]Candidate, error) {
	decided, err := s.decide(ctx, file, debug, candidates)
	if err != nil {
		if s.FailOnError || !s.PassThroughOnSkip || ctx.Err() != nil || unrecoverable(err) {
			return nil, err
		}

		return candidates, &Skipped{Err: err}
	}

	if debug.DebugEnabled() {
		s.report(debug, decided)
	}

	return slices.Concat(decided.kept, decided.passed), nil
}

func unrecoverable(err error) bool {
	stageErr, ok := errors.AsType[*Error](err)
	return ok && stageErr.Unrecoverable
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

	decided := partition(candidates, scores, floor)
	decided.kept = decided.qualifying

	if s.MaxKeep > 0 && len(decided.kept) > s.MaxKeep {
		decided.kept = decided.kept[:s.MaxKeep]
	}

	return decided, nil
}

func partition(candidates []Candidate, scores []float64, floor Threshold) decision {
	d := decision{received: len(candidates), floor: floor}
	for i, candidate := range candidates {
		if math.IsInf(scores[i], 1) {
			d.passed = append(d.passed, candidate)
			continue
		}

		candidate.Score = scores[i]
		if candidate.Score >= floor.For(candidate.ADR) {
			d.qualifying = append(d.qualifying, candidate)
		} else {
			d.below = append(d.below, candidate)
		}
	}

	sort.SliceStable(d.qualifying, func(i, j int) bool { return d.qualifying[i].Score > d.qualifying[j].Score })
	sort.SliceStable(d.below, func(i, j int) bool { return d.below[i].Score > d.below[j].Score })

	return d
}

func (d decision) cut() []Candidate { return d.qualifying[len(d.kept):] }

func (s Stage) report(debug Debug, d decision) {
	debug.Debug("Stage %s: %d candidate(s) received, %d kept", s.Name, d.received, len(d.kept)+len(d.passed))
	for _, candidate := range d.kept {
		debug.Debug("Kept: %s (score %.2f)", candidate.ADR.Title, candidate.Score)
	}

	for _, candidate := range d.passed {
		debug.Debug("Kept: %s (not scored by this stage)", candidate.ADR.Title)
	}

	if s.ListRejected {
		reportEachRejected(debug, d)
		return
	}

	if len(d.below) > 0 {
		thresholds := make([]float64, len(d.below))
		for i, candidate := range d.below {
			thresholds[i] = d.floor.For(candidate.ADR)
		}

		debug.Debug("Below threshold: %d ADR(s), %s (%s)", len(d.below), spread("score", scoresOf(d.below)), spread("threshold", thresholds))
	}

	if cut := d.cut(); len(cut) > 0 {
		debug.Debug("Cut by top-K limit: %d ADR(s), %s (limit %d)", len(cut), spread("score", scoresOf(cut)), s.MaxKeep)
	}
}

func reportEachRejected(debug Debug, d decision) {
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

func scoresOf(candidates []Candidate) []float64 {
	scores := make([]float64, len(candidates))
	for i, candidate := range candidates {
		scores[i] = candidate.Score
	}

	return scores
}

// spread prints "score 0.71" when the values round alike, else "scores 0.31 to 0.59".
func spread(noun string, values []float64) string {
	low := fmt.Sprintf("%.2f", slices.Min(values))
	high := fmt.Sprintf("%.2f", slices.Max(values))

	if low == high {
		return noun + " " + low
	}

	return fmt.Sprintf("%ss %s to %s", noun, low, high)
}
