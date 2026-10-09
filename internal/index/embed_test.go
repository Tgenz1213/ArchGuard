package index

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func newEmbedJob(adrs []ADR, embed func(text string) ([]float32, error)) embedJob {
	provider := &inference.MockProvider{
		EmbedFunc: func(_ context.Context, text string, _ inference.EmbeddingTaskType) ([]float32, error) {
			return embed(text)
		},
	}

	return embedJob{adrs: adrs, embedder: provider, out: output.New(&bytes.Buffer{}, false)}
}

func TestEmbedJob_Run(t *testing.T) {
	failOn := func(title string) func(string) ([]float32, error) {
		return func(text string) ([]float32, error) {
			if strings.Contains(text, "Title: "+title+"\n") {
				return nil, errors.New("rejected")
			}

			return []float32{1}, nil
		}
	}

	tests := []struct {
		name        string
		label       string
		concurrency int
		failTitle   string
		wantFailed  []int
		wantErrText string
	}{
		{name: "all succeed", concurrency: 2},
		{name: "one failure is isolated", concurrency: 2, failTitle: "b", wantFailed: []int{1}, wantErrText: "rejected"},
		{name: "label tags the embed error", label: "embed", failTitle: "a", wantFailed: []int{0}, wantErrText: "embed: rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adrs := []ADR{{RelPath: "a.md", Title: "a"}, {RelPath: "b.md", Title: "b"}, {RelPath: "c.md", Title: "c"}}
			job := newEmbedJob(adrs, failOn(tt.failTitle))
			job.concurrency = tt.concurrency
			job.embedLabel = tt.label

			outcome, err := job.run(t.Context(), []int{0, 1, 2})
			require.NoError(t, err)

			assert.Len(t, outcome.failed, len(tt.wantFailed))
			assert.Len(t, outcome.skipped, len(tt.wantFailed))

			for _, idx := range tt.wantFailed {
				assert.True(t, outcome.failed[idx])
				assert.Nil(t, adrs[idx].Embedding)
				assert.EqualError(t, outcome.skipped[0].Err, tt.wantErrText)
			}

			for idx := range adrs {
				if !outcome.failed[idx] {
					assert.Equal(t, []float32{1}, adrs[idx].Embedding)
				}
			}
		})
	}
}

func TestEmbedJob_Run_BoundsConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
		wantMax     int32
	}{
		{name: "explicit limit", concurrency: 2, wantMax: 2},
		{name: "zero uses the default", concurrency: 0, wantMax: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adrs := make([]ADR, 12)
			indices := make([]int, len(adrs))

			for i := range adrs {
				indices[i] = i
			}

			var (
				mu               sync.Mutex
				inFlight, peakIn int32
			)

			job := newEmbedJob(adrs, func(string) ([]float32, error) {
				mu.Lock()
				inFlight++
				peakIn = max(peakIn, inFlight)
				mu.Unlock()

				time.Sleep(20 * time.Millisecond)

				mu.Lock()
				inFlight--
				mu.Unlock()

				return []float32{1}, nil
			})
			job.concurrency = tt.concurrency

			_, err := job.run(t.Context(), indices)

			require.NoError(t, err)
			assert.Equal(t, tt.wantMax, peakIn)
		})
	}
}

func TestEmbedJob_Run_NoIndicesIsANoOp(t *testing.T) {
	job := newEmbedJob(nil, func(string) ([]float32, error) {
		t.Fatal("embedder must not be called")
		return nil, nil
	})

	outcome, err := job.run(t.Context(), nil)

	require.NoError(t, err)
	assert.Empty(t, outcome.failed)
	assert.Empty(t, outcome.skipped)
}

func TestEmbedJob_Run_PersistsOnlyEmbeddedADRs(t *testing.T) {
	adrs := []ADR{{RelPath: "a.md", Title: "a"}, {RelPath: "b.md", Title: "b"}}
	job := newEmbedJob(adrs, func(text string) ([]float32, error) {
		if strings.Contains(text, "Title: a\n") {
			return nil, errors.New("rejected")
		}

		return []float32{1}, nil
	})

	var (
		mu        sync.Mutex
		persisted []string
	)

	job.persist = func(_ context.Context, adr ADR) error {
		mu.Lock()
		defer mu.Unlock()

		persisted = append(persisted, adr.RelPath)

		return nil
	}

	_, err := job.run(t.Context(), []int{0, 1})

	require.NoError(t, err)
	assert.Equal(t, []string{"b.md"}, persisted)
}

func TestEmbedJob_EmbedInto(t *testing.T) {
	persist := func(context.Context, ADR) error { return nil }

	tests := []struct {
		name        string
		persist     func(context.Context, ADR) error
		failTitles  []string
		wantValid   int
		wantSkipped int
		wantErr     string
	}{
		{name: "all embedded", wantValid: 2},
		{name: "partial failure counts only indexed ADRs", failTitles: []string{"a"}, wantValid: 1, wantSkipped: 1},
		{name: "all failed without persist", failTitles: []string{"a", "b"}, wantSkipped: 2, wantErr: "all 2 ADR(s) failed to embed; index not updated"},
		{name: "all failed with persist", persist: persist, failTitles: []string{"a", "b"}, wantSkipped: 2, wantErr: "all 2 ADR(s) failed to embed or persist; index not updated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adrs := []ADR{{RelPath: "a.md", Title: "a"}, {RelPath: "b.md", Title: "b"}}
			job := newEmbedJob(adrs, func(text string) ([]float32, error) {
				for _, title := range tt.failTitles {
					if strings.Contains(text, "Title: "+title+"\n") {
						return nil, errors.New("rejected")
					}
				}

				return []float32{1}, nil
			})
			job.persist = tt.persist

			var result BuildIndexResult

			_, err := job.embedInto(t.Context(), []int{0, 1}, &result)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}

			assert.Equal(t, tt.wantValid, result.Valid)
			assert.Len(t, result.Skipped, tt.wantSkipped)
		})
	}
}

func TestEmbedJob_Run_PersistErrorSkipsADR(t *testing.T) {
	adrs := []ADR{{RelPath: "a.md", Title: "a"}, {RelPath: "b.md", Title: "b"}}
	job := newEmbedJob(adrs, func(string) ([]float32, error) { return []float32{1}, nil })
	job.persist = func(_ context.Context, adr ADR) error {
		if adr.RelPath == "a.md" {
			return errors.New("upsert: boom")
		}

		return nil
	}

	outcome, err := job.run(t.Context(), []int{0, 1})

	require.NoError(t, err)
	assert.Equal(t, map[int]bool{0: true}, outcome.failed)
	require.Len(t, outcome.skipped, 1)
	assert.Equal(t, "a.md", outcome.skipped[0].RelPath)
	assert.EqualError(t, outcome.skipped[0].Err, "upsert: boom")
}

func TestEmbedJob_Run_PersistSeesTheEmbedding(t *testing.T) {
	adrs := []ADR{{RelPath: "a.md", Title: "a"}}
	job := newEmbedJob(adrs, func(string) ([]float32, error) { return []float32{7}, nil })

	var persisted []float32

	job.persist = func(_ context.Context, adr ADR) error {
		persisted = adr.Embedding
		return nil
	}

	_, err := job.run(t.Context(), []int{0})

	require.NoError(t, err)
	assert.Equal(t, []float32{7}, persisted)
}

func TestEmbedOutcome_Check(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		failed  map[int]bool
		total   int
		wantErr string
	}{
		{name: "no failures", ctx: t.Context(), total: 2},
		{name: "partial failure is tolerated", ctx: t.Context(), failed: map[int]bool{0: true}, total: 2},
		{name: "cancelled context wins over all failed", ctx: cancelled, failed: map[int]bool{0: true, 1: true}, total: 2, wantErr: context.Canceled.Error()},
		{name: "every ADR failed", ctx: t.Context(), failed: map[int]bool{0: true, 1: true}, total: 2, wantErr: "all 2 ADR(s) failed to embed; index not updated"},
		{name: "empty corpus is not all-failed", ctx: t.Context(), total: 0},
		{name: "cancelled context with nothing to embed", ctx: cancelled, total: 2, wantErr: context.Canceled.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := embedOutcome{failed: tt.failed}.check(tt.ctx, tt.total, "embed")

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestAdrUnchanged(t *testing.T) {
	base := ADR{Content: "c", Title: "t", Status: "s"}

	tests := []struct {
		name    string
		current ADR
		want    bool
	}{
		{name: "same", current: base, want: true},
		{name: "content differs", current: ADR{Content: "x", Title: "t", Status: "s"}},
		{name: "title differs", current: ADR{Content: "c", Title: "x", Status: "s"}},
		{name: "status differs", current: ADR{Content: "c", Title: "t", Status: "x"}},
		{name: "scope differs but is not embedded", current: ADR{Content: "c", Title: "t", Status: "s", Scope: []string{"**"}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adrUnchanged(base, tt.current))
		})
	}
}
