package index

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tgenz1213/archguard/internal/inference"
)

func failingTitleProvider(title string, err error, dim int) *inference.MockProvider {
	return &inference.MockProvider{
		EmbedFunc: func(_ context.Context, text string, _ inference.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "Title: "+title+"\n") {
				return nil, err
			}

			return make([]float32, dim), nil
		},
	}
}

func TestLocalStore_BuildIndex_SkippedErrorIsTheEmbedderError(t *testing.T) {
	embedErr := errors.New("provider rejected the input")
	adrs := []ADR{{RelPath: "a.md", Title: "A"}, {RelPath: "b.md", Title: "B"}}

	result, err := NewLocalStore(2).BuildIndex(t.Context(), "model", 2, failingTitleProvider("A", embedErr, 2), &mockADRProvider{adrs: adrs})

	require.NoError(t, err)
	require.Len(t, result.Skipped, 1)
	assert.Same(t, embedErr, result.Skipped[0].Err)
}

func TestLocalStore_BuildIndex_HashCoversSkippedADRs(t *testing.T) {
	adrs := []ADR{{RelPath: "a.md", Title: "A", Content: "a"}, {RelPath: "b.md", Title: "B", Content: "b"}}
	store := NewLocalStore(2)

	_, err := store.BuildIndex(t.Context(), "model", 2, failingTitleProvider("B", errors.New("rejected"), 2), &mockADRProvider{adrs: adrs})
	require.NoError(t, err)
	require.Len(t, store.ADRs, 1)

	wantHash, err := store.CalculateHash(adrs, "model")
	require.NoError(t, err)
	assert.Equal(t, wantHash, store.Hash)
}

func TestLocalStore_BuildIndex_DimFallsBackToFirstKeptEmbedding(t *testing.T) {
	adrs := []ADR{{RelPath: "a.md", Title: "A"}, {RelPath: "b.md", Title: "B"}}
	store := NewLocalStore(1)

	_, err := store.BuildIndex(t.Context(), "model", 0, failingTitleProvider("A", errors.New("rejected"), 3), &mockADRProvider{adrs: adrs})

	require.NoError(t, err)
	assert.Equal(t, 3, store.Dim)
}
