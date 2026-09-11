package index

import "sort"

// filterByScope keeps only ADRs with no scope restriction or a scope glob
// matching filePath -- applied before rankAndLimit, not after.
func filterByScope(candidates []SearchResult, filePath string) []SearchResult {
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.ADR.Scope == "" || MatchGlob(c.ADR.Scope, filePath) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// rankAndLimit sorts candidates by descending similarity score and
// truncates to at most topK.
func rankAndLimit(candidates []SearchResult, topK int) []SearchResult {
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > topK {
		return candidates[:topK]
	}
	return candidates
}
