package index_test

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRandomVector_ReturnsRequestedDimension(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	v := randomVector(rng, 1536)
	assert.Len(t, v, 1536)
	for _, x := range v {
		assert.GreaterOrEqual(t, x, float32(-1))
		assert.LessOrEqual(t, x, float32(1))
	}
}

func TestComputeRecall(t *testing.T) {
	tests := []struct {
		name        string
		hnsw        []string
		groundTruth []string
		want        float64
	}{
		{"perfect match", []string{"a.md", "b.md", "c.md"}, []string{"a.md", "b.md", "c.md"}, 1.0},
		{"partial match", []string{"a.md", "x.md"}, []string{"a.md", "b.md"}, 0.5},
		{"no match", []string{"x.md", "y.md"}, []string{"a.md", "b.md"}, 0.0},
		{"empty ground truth is vacuously full recall", []string{"a.md"}, []string{}, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeRecall(tt.hnsw, tt.groundTruth))
		})
	}
}

func TestLatencyPercentiles(t *testing.T) {
	latencies := []time.Duration{
		100 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		200 * time.Millisecond,
		20 * time.Millisecond,
	}
	// sorted: 10, 20, 50, 100, 200 (ms) -- len=5, p50 idx=2 (50ms), p95 idx=4 (200ms)
	p50, p95 := latencyPercentiles(latencies)
	assert.Equal(t, 50*time.Millisecond, p50)
	assert.Equal(t, 200*time.Millisecond, p95)
}

func TestLatencyPercentiles_Empty(t *testing.T) {
	p50, p95 := latencyPercentiles(nil)
	assert.Equal(t, time.Duration(0), p50)
	assert.Equal(t, time.Duration(0), p95)
}

func randomVector(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(rng.Float64()*2 - 1)
	}
	return v
}

// computeRecall is the fraction of groundTruthRelPaths also in hnswRelPaths; empty ground truth is vacuous full recall.
func computeRecall(hnswRelPaths, groundTruthRelPaths []string) float64 {
	if len(groundTruthRelPaths) == 0 {
		return 1.0
	}

	inGroundTruth := make(map[string]bool, len(groundTruthRelPaths))
	for _, rp := range groundTruthRelPaths {
		inGroundTruth[rp] = true
	}

	matched := 0
	for _, rp := range hnswRelPaths {
		if inGroundTruth[rp] {
			matched++
		}
	}
	return float64(matched) / float64(len(groundTruthRelPaths))
}

func latencyPercentiles(latencies []time.Duration) (p50, p95 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	p50 = sorted[len(sorted)*50/100]
	idx95 := len(sorted) * 95 / 100
	if idx95 >= len(sorted) {
		idx95 = len(sorted) - 1
	}
	p95 = sorted[idx95]
	return p50, p95
}
