package index

import (
	"math"
)

type SearchResult struct {
	ADR   *ADR
	Score float64
}

func (s *LocalStore) ScopedADRs(filePath string) []SearchResult {
	candidates := make([]SearchResult, 0, len(s.ADRs))
	for i := range s.ADRs {
		candidates = append(candidates, SearchResult{ADR: &s.ADRs[i]})
	}
	return filterByScope(candidates, filePath)
}

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

func (s *LocalStore) Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
}

func (s *LocalStore) SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)
	candidates = filterBelowThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
}

func (s *LocalStore) SearchTruncated(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	candidates := s.scopeMatchedCandidates(queryEmbedding, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return truncatedByTopK(candidates, topK)
}

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
