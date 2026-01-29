package index

import (
	"math"
	"sort"
)

// SearchResult represents an ADR matched during a vector search with its similarity score.
type SearchResult struct {
	ADR   *ADR
	Score float64
}

// Search performs a vector similarity search across the store, returning up to topK results
// that meet or exceed the specified threshold.
func (s *Store) Search(queryEmbedding []float32, threshold float64, topK int) []SearchResult {
	var results []SearchResult

	for i := range s.ADRs {
		score := cosineSimilarity(queryEmbedding, s.ADRs[i].Embedding)
		if score >= threshold {
			results = append(results, SearchResult{
				ADR:   &s.ADRs[i],
				Score: score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		return results[:topK]
	}
	return results
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
