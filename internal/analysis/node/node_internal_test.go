package node

import (
	"context"
	"slices"
	"testing"

	"github.com/tgenz1213/archguard/internal/index"
)

type fixedScorer []float64

func (f fixedScorer) Score(context.Context, File, Debug, []Candidate) ([]float64, error) {
	return f, nil
}

type noFile struct{}

func (noFile) Path() string                     { return "" }
func (noFile) QueryText(context.Context) string { return "" }
func (noFile) Sides() []string                  { return nil }

func newCandidates(ids ...string) []Candidate {
	out := make([]Candidate, len(ids))
	for i, id := range ids {
		out[i] = Candidate{ADR: &index.ADR{ID: id}}
	}

	return out
}

func candidateIDs(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ADR.ID
	}

	return out
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		node       Node
		candidates []Candidate
		received   int
		kept       []string
		below      []string
		cut        []string
	}{
		{
			name:       "separates below-threshold drops from the top-K cut",
			node:       Node{Scorer: fixedScorer{0.9, 0.8, 0.7, 0.1}, Min: FixedMin(0.5), MaxKeep: 2},
			candidates: newCandidates("a", "b", "c", "d"),
			received:   4,
			kept:       []string{"a", "b"},
			below:      []string{"d"},
			cut:        []string{"c"},
		},
		{
			name:       "records every below-threshold drop best first",
			node:       Node{Scorer: fixedScorer{0.1, 0.2, 0.3, 0.4}, Min: FixedMin(0.5), MaxKeep: 2},
			candidates: newCandidates("a", "b", "c", "d"),
			received:   4,
			kept:       []string{},
			below:      []string{"d", "c", "b", "a"},
			cut:        []string{},
		},
		{
			name:     "received nothing",
			node:     Node{Scorer: fixedScorer{}},
			received: 0,
			kept:     []string{},
			below:    []string{},
			cut:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := tt.node.decide(t.Context(), noFile{}, NoDebug, tt.candidates)
			if err != nil {
				t.Fatalf("decide: %v", err)
			}

			if d.received != tt.received {
				t.Errorf("received = %d, want %d", d.received, tt.received)
			}

			if got := candidateIDs(d.kept); !slices.Equal(got, tt.kept) {
				t.Errorf("kept = %v, want %v", got, tt.kept)
			}

			if got := candidateIDs(d.below); !slices.Equal(got, tt.below) {
				t.Errorf("below = %v, want %v", got, tt.below)
			}

			if got := candidateIDs(d.cut()); !slices.Equal(got, tt.cut) {
				t.Errorf("cut = %v, want %v", got, tt.cut)
			}
		})
	}
}
