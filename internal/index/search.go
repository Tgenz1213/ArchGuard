package index

import (
	"math"
)

// SearchResult represents an ADR matched during a vector search with its similarity score.
type SearchResult struct {
	ADR   *ADR
	Score float64
}

// Search returns up to topK ADRs whose scope (if any) matches filePath and
// whose similarity is at least threshold -- scope is filtered first, then
// threshold, then the topK cut (see filterByScope, filterByThreshold, rankAndLimit).
func (s *LocalStore) Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	var candidates []SearchResult

	for i := range s.ADRs {
		candidates = append(candidates, SearchResult{
			ADR:   &s.ADRs[i],
			Score: cosineSimilarity(queryEmbedding, s.ADRs[i].Embedding),
		})
	}

	candidates = filterByScope(candidates, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
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
