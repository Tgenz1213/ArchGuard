package node

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

// Telemetry runs a pipeline's nodes and totals each one's candidates and time; safe for concurrent Apply calls.
type Telemetry struct {
	nodes []Node

	mu    sync.Mutex
	stats []Stats
	spent []time.Duration
}

func NewTelemetry(nodes []Node) *Telemetry {
	t := &Telemetry{nodes: nodes, stats: make([]Stats, len(nodes)), spent: make([]time.Duration, len(nodes))}
	for i := range nodes {
		t.stats[i].Name = nodes[i].Name
	}

	return t
}

// A failed node still counts what it received and how long it ran, with nothing kept.
func (t *Telemetry) Apply(ctx context.Context, nodeIndex int, file File, debug Debug, candidates []Candidate) ([]Candidate, error) {
	start := time.Now()
	kept, err := t.nodes[nodeIndex].Apply(ctx, file, debug, candidates)
	elapsed := time.Since(start)

	t.mu.Lock()
	t.stats[nodeIndex].Received += len(candidates)
	t.stats[nodeIndex].Kept += len(kept)

	if len(candidates) > 0 {
		t.spent[nodeIndex] += elapsed
	}

	t.mu.Unlock()
	return kept, err
}

func (t *Telemetry) Stats() []Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Stats, len(t.stats))
	for i, entry := range t.stats {
		entry.DurationMS = t.spent[i].Milliseconds()
		out[i] = entry
	}

	return out
}
