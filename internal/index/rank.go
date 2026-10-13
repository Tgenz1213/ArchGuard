package index

import "sort"

// filterByScope keeps ADRs with no scope restriction or a scope glob matching
// filePath, overwriting candidates in place (call before rankAndLimit, not after).
func filterByScope(candidates []SearchResult, filePath string) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.ADR.Scope.Matches(filePath) {
			filtered = append(filtered, c)
		}
	}

	return filtered
}

func EffectiveThreshold(adr *ADR, global float64) float64 {
	if adr.SimilarityThreshold != nil {
		return *adr.SimilarityThreshold
	}

	return global
}

// Shared by both threshold filters so they can't drift apart.
func meetsThreshold(c SearchResult, global float64) bool {
	return c.Score >= EffectiveThreshold(c.ADR, global)
}

func filterByThreshold(candidates []SearchResult, threshold float64) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if meetsThreshold(c, threshold) {
			filtered = append(filtered, c)
		}
	}

	return filtered
}

// Debug only.
func filterBelowThreshold(candidates []SearchResult, threshold float64) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if !meetsThreshold(c, threshold) {
			filtered = append(filtered, c)
		}
	}

	return filtered
}

func rankAndLimit(candidates []SearchResult, topK int) []SearchResult {
	if topK < 0 {
		topK = 0
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })

	if len(candidates) > topK {
		return candidates[:topK]
	}

	return candidates
}

// Debug only: the candidates rankAndLimit cuts.
func truncatedByTopK(candidates []SearchResult, topK int) []SearchResult {
	if topK < 0 {
		topK = 0
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })

	if len(candidates) > topK {
		return candidates[topK:]
	}

	return nil
}

// The threshold filters reuse their input's backing array, so each set filters its own copy.
func splitDebugResults(candidates []SearchResult, threshold float64, topK int) (hits, rejected, truncated []SearchResult) {
	belowCopy := append([]SearchResult(nil), candidates...)
	rejected = rankAndLimit(filterBelowThreshold(belowCopy, threshold), topK)

	qualifying := filterByThreshold(append([]SearchResult(nil), candidates...), threshold)
	hits = rankAndLimit(qualifying, topK)
	truncated = truncatedByTopK(qualifying, topK)

	return hits, rejected, truncated
}
