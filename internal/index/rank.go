package index

import "sort"

// filterByScope narrows candidates to those an ADR structurally applies to:
// no scope restriction, or a scope glob that matches filePath. Applied
// before rankAndLimit so a scope-matching ADR can't be excluded just
// because higher-similarity ADRs with a non-matching scope crowd it out
// of the topK cut. See docs/arch/0007-scope-filtered-before-topk-similarity.md.
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
