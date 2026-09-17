package index

import (
	"math"
)

// SearchResult represents an ADR matched during a vector search with its similarity score.
type SearchResult struct {
	ADR   *ADR
	Score float64
}

// scopeMatchedCandidates scores every ADR against queryEmbedding and keeps
// those whose scope (if any) matches filePath -- the shared starting point
// for Search, SearchRejected, SearchTruncated, and SearchWithDebugInfo.
func (s *LocalStore) scopeMatchedCandidates(queryEmbedding []float32, filePath string) []SearchResult {
	var candidates []SearchResult

	for i := range s.ADRs {
		candidates = append(candidates, SearchResult{
			ADR:   &s.ADRs[i],
			Score: cosineSimilarity(queryEmbedding, s.ADRs[i].Embedding),
		})
	}

	return filterByScope(candidates, filePath)
}

// Search returns up to topK ADRs whose scope (if any) matches filePath and
// whose similarity is at least threshold, before the topK cut.
func (s *LocalStore) Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
}

// SearchRejected returns up to topK scope-matched ADRs that scored below
// threshold, ranked by descending similarity -- for --debug diagnostics only.
func (s *LocalStore) SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)
	candidates = filterBelowThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
}

// SearchTruncated returns scope-matched, threshold-passing candidates that
// rankAndLimit cut purely for exceeding topK -- Search's other complement,
// alongside SearchRejected, for --debug diagnostics only.
func (s *LocalStore) SearchTruncated(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return truncatedByTopK(candidates, topK)
}

// SearchWithDebugInfo derives hits, rejected, and truncated from one
// scope-matched candidate set, so all three are guaranteed consistent with
// each other -- see the VectorStore interface doc for why that matters.
func (s *LocalStore) SearchWithDebugInfo(queryEmbedding []float32, threshold float64, topK int, filePath string) (hits, rejected, truncated []SearchResult) {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)

	belowCopy := append([]SearchResult(nil), candidates...)
	rejected = rankAndLimit(filterBelowThreshold(belowCopy, threshold), topK)

	qualifying := filterByThreshold(append([]SearchResult(nil), candidates...), threshold)
	hits = rankAndLimit(qualifying, topK)
	truncated = truncatedByTopK(qualifying, topK)

	return hits, rejected, truncated
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
