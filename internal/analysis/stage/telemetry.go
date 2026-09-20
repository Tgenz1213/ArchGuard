package stage

import (
	"context"
	"sync"
	"time"
)

// DurationMS sums per-file time, so concurrent files can make it exceed wall-clock time.
type Stats struct {
	Name       string `json:"name"`
	Received   int    `json:"received"`
	Kept       int    `json:"kept"`
	DurationMS int64  `json:"duration_ms"`
}

// Telemetry runs a pipeline's stages and totals each one's candidates and time; safe for concurrent Apply calls.
type Telemetry struct {
	stages []Stage

	mu    sync.Mutex
	stats []Stats
	spent []time.Duration
}

func NewTelemetry(stages []Stage) *Telemetry {
	t := &Telemetry{stages: stages, stats: make([]Stats, len(stages)), spent: make([]time.Duration, len(stages))}
	for i, s := range stages {
		t.stats[i].Name = s.Name
	}
	return t
}

// A failed stage still counts what it received and how long it ran, with nothing kept.
func (t *Telemetry) Apply(ctx context.Context, i int, file File, debug Debug, candidates []Candidate) ([]Candidate, error) {
	start := time.Now()
	kept, err := t.stages[i].Apply(ctx, file, debug, candidates)
	elapsed := time.Since(start)

	t.mu.Lock()
	t.stats[i].Received += len(candidates)
	t.stats[i].Kept += len(kept)
	t.spent[i] += elapsed
	t.mu.Unlock()
	return kept, err
}

func (t *Telemetry) Stats() []Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Stats, len(t.stats))
	for i, s := range t.stats {
		s.DurationMS = t.spent[i].Milliseconds()
		out[i] = s
	}
	return out
}
