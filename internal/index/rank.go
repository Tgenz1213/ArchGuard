package index

import "sort"

// filterByScope keeps ADRs with no scope restriction or a scope glob matching
// filePath, overwriting candidates in place (call before rankAndLimit, not after).
func filterByScope(candidates []SearchResult, filePath string) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.ADR.Scope == "" || MatchGlob(c.ADR.Scope, filePath) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// filterByThreshold keeps candidates whose Score is at least threshold,
// mirroring filterByScope's placement ahead of rankAndLimit.
func filterByThreshold(candidates []SearchResult, threshold float64) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.Score >= threshold {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// filterBelowThreshold keeps candidates whose Score is below threshold --
// filterByThreshold's complement, for --debug diagnostics only.
func filterBelowThreshold(candidates []SearchResult, threshold float64) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.Score < threshold {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// rankAndLimit sorts candidates by descending similarity score and
// truncates to at most topK.
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
