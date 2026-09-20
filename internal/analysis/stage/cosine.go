package stage

import (
	"context"
	"errors"
	"math"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

// The store omits candidates below their own effective threshold; this score lets any stage minimum drop them.
const unscored = -1

type CosineRanker struct {
	Store     index.VectorStore
	Embed     llm.Embedder
	Threshold float64
}

func (c *CosineRanker) Score(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]float64, error) {
	if c.Embed == nil {
		return nil, &Error{Action: "generating embedding", Kind: KindPreconditionNotMet, Err: errors.New("no embedding provider configured")}
	}
	embedding, err := c.Embed.CreateEmbedding(ctx, file.QueryText(), llm.EmbeddingTaskQuery)
	if err != nil {
		return nil, &Error{Action: "generating embedding", Err: err}
	}

	// Unbounded topK: the Stage applies the top-K cut.
	var found []index.SearchResult
	if debug.Enabled() {
		hits, rejected, _ := c.Store.SearchWithDebugInfo(embedding, c.Threshold, math.MaxInt32, file.Path())
		found = append(hits, rejected...)
	} else {
		found = c.Store.Search(embedding, c.Threshold, math.MaxInt32, file.Path())
	}

	byKey := make(map[string]float64, len(found))
	for _, r := range found {
		byKey[adrKey(r.ADR)] = r.Score
	}

	scores := make([]float64, len(candidates))
	for i, cand := range candidates {
		score, ok := byKey[adrKey(cand.ADR)]
		if !ok {
			score = unscored
		}
		scores[i] = score
	}
	return scores, nil
}

func adrKey(adr *index.ADR) string {
	return adr.RelPath + "\x00" + adr.ID + "\x00" + adr.Title
}

func NewCosineStage(store index.VectorStore, embed llm.Embedder, threshold float64, topK int) Stage {
	return Stage{
		Name:    "rank",
		Scorer:  &CosineRanker{Store: store, Embed: embed, Threshold: threshold},
		Min:     ADRThreshold{Global: threshold},
		MaxKeep: topK,
	}
}
