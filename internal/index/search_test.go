package index

import (
	"path/filepath"
	"testing"
)

// reproduces #134: a lower-similarity scope-matching ADR must still be
// evaluated over 3+ higher-similarity non-matching-scope ADRs.
func TestLocalStore_Search_ScopeMatchingADRSurvivesDespiteLowerSimilarity(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "Distractor A", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Distractor B", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Distractor C", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: ScopePatterns{"**/*.go"}, Embedding: []float32{1, 1}},
	}

	// "Scope Match" has lower similarity (~0.707) than the distractors
	// (1.0), so pre-fix rank-then-filter code would have dropped it.
	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result (the scope-matching ADR), got %d: %+v", len(results), results)
	}
	if results[0].ADR.Title != "Scope Match" {
		t.Errorf("expected the scope-matching ADR to be returned, got %q", results[0].ADR.Title)
	}
}

func TestLocalStore_Search_ZeroCandidatesAfterScopeFilter(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "TS only", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "JS only", Scope: ScopePatterns{"**/*.js"}, Embedding: []float32{1, 0}},
	}

	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	if len(results) != 0 {
		t.Fatalf("expected no results once scope filtering excludes every ADR, got %d: %+v", len(results), results)
	}
}

func TestLocalStore_Search_RespectsThresholdAndTopK(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "high", Embedding: []float32{1, 0}},
		{Title: "mid", Embedding: []float32{1, 1}},
		{Title: "below threshold", Embedding: []float32{0, 1}},
	}

	// Query [1,0]: sim("high")=1.0, sim("mid")=~0.707, sim("below
	// threshold")=0.0 -- excluded by a 0.5 threshold.
	results := store.Search([]float32{1, 0}, 0.5, 1, "any.go")

	if len(results) != 1 {
		t.Fatalf("expected topK=1 result, got %d", len(results))
	}
	if results[0].ADR.Title != "high" {
		t.Errorf("expected the highest-similarity ADR within threshold, got %q", results[0].ADR.Title)
	}
}

// documents the #140 ordering: filterByScope and filterByThreshold both
// run before rankAndLimit, though a below-threshold ADR is excluded either way.
func TestLocalStore_Search_ScopeMatchingADRSurvivesDespiteBelowThresholdSimilarity(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "Distractor A", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Distractor B", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Distractor C", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: ScopePatterns{"**/*.go"}, Embedding: []float32{0, 1}},
	}

	// "Scope Match" has 0.0 similarity to the query (below the 0.5
	// threshold), but it's the only ADR scoped to "service.go".
	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	if len(results) != 0 {
		t.Fatalf("expected 0 results: the scope-matching ADR is a candidate but still below threshold, got %d: %+v", len(results), results)
	}
}

// A scope-matching ADR that also clears threshold must be returned.
func TestLocalStore_Search_ScopeMatchingADRAboveThresholdSurvives(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "Distractor A", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: ScopePatterns{"**/*.go"}, Embedding: []float32{1, 1}},
	}

	// "Scope Match" has ~0.707 similarity -- above a 0.5 threshold -- and
	// is the only ADR scoped to "service.go".
	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	if len(results) != 1 || results[0].ADR.Title != "Scope Match" {
		t.Fatalf("expected exactly [Scope Match], got %+v", results)
	}
}

func TestLocalStore_SearchRejected_ReturnsClosestBelowThreshold(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "high", Embedding: []float32{1, 0}},
		{Title: "near miss", Embedding: []float32{1, 1}},
		{Title: "far miss", Embedding: []float32{0, 1}},
	}

	// Query [1,0]: sim("high")=1.0 (passes 0.75), sim("near miss")=~0.707
	// (rejected, closest reject), sim("far miss")=0.0 (rejected, furthest).
	rejected := store.SearchRejected([]float32{1, 0}, 0.75, 3, "any.go")

	if len(rejected) != 2 {
		t.Fatalf("expected 2 rejected candidates, got %d: %+v", len(rejected), rejected)
	}
	if rejected[0].ADR.Title != "near miss" {
		t.Errorf("expected closest reject first, got %q", rejected[0].ADR.Title)
	}
	if rejected[1].ADR.Title != "far miss" {
		t.Errorf("expected furthest reject last, got %q", rejected[1].ADR.Title)
	}
}

func TestLocalStore_SearchRejected_RespectsScopeAndTopK(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "wrong scope", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 1}},
		{Title: "right scope", Scope: ScopePatterns{"**/*.go"}, Embedding: []float32{1, 1}},
	}

	// Both score ~0.707, below a 0.9 threshold; only "right scope" matches service.go.
	rejected := store.SearchRejected([]float32{1, 0}, 0.9, 3, "service.go")

	if len(rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected candidate (scope filters out the other), got %d: %+v", len(rejected), rejected)
	}
	if rejected[0].ADR.Title != "right scope" {
		t.Errorf("expected the scope-matching ADR, got %q", rejected[0].ADR.Title)
	}
}

func TestLocalStore_Search_MultiPatternScopeSurvivesSaveLoadRoundTrip(t *testing.T) {
	store := NewLocalStore(1)
	store.ModelName = "test-model"
	store.Dim = 2
	store.Hash = "test-hash"
	store.ADRs = []ADR{
		{Title: "Multi Scope", Scope: ScopePatterns{"internal/api/**", "internal/handlers/**"}, Embedding: []float32{1, 1}},
	}

	path := filepath.Join(t.TempDir(), "index.json")
	if err := store.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := NewLocalStore(1)
	if err := loaded.Load(path, "test-model", 2, "test-hash"); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	results := loaded.Search([]float32{1, 0}, 0.5, 3, "internal/handlers/foo.go")

	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d: %+v", len(results), results)
	}
	if results[0].ADR.Title != "Multi Scope" {
		t.Errorf("expected 'Multi Scope' ADR, got %q", results[0].ADR.Title)
	}
	if len(results[0].ADR.Scope) != 2 || results[0].ADR.Scope[0] != "internal/api/**" || results[0].ADR.Scope[1] != "internal/handlers/**" {
		t.Errorf("expected both scope patterns to survive save/load round-trip, got %+v", results[0].ADR.Scope)
	}
}

func TestLocalStore_SearchRejected_EmptyWhenNothingBelowThreshold(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "high", Embedding: []float32{1, 0}},
	}

	rejected := store.SearchRejected([]float32{1, 0}, 0.5, 3, "any.go")

	if len(rejected) != 0 {
		t.Fatalf("expected no rejected candidates, got %d: %+v", len(rejected), rejected)
	}
}

func TestLocalStore_SearchTruncated_ReturnsAboveThresholdCandidatesCutByTopK(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "first", Embedding: []float32{1, 0}},
		{Title: "second", Embedding: []float32{1, 1}},
		{Title: "third", Embedding: []float32{2, 1}},
		{Title: "fourth", Embedding: []float32{3, 1}},
	}

	// Query [1,0]: all four clear a 0.1 threshold; topK=2 keeps the top 2
	// and should truncate the other 2.
	truncated := store.SearchTruncated([]float32{1, 0}, 0.1, 2, "any.go")

	if len(truncated) != 2 {
		t.Fatalf("expected 2 truncated candidates, got %d: %+v", len(truncated), truncated)
	}
	hits := store.Search([]float32{1, 0}, 0.1, 2, "any.go")
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits from Search, got %d: %+v", len(hits), hits)
	}
	for _, h := range hits {
		for _, tr := range truncated {
			if h.ADR.Title == tr.ADR.Title {
				t.Errorf("ADR %q returned by both Search and SearchTruncated", h.ADR.Title)
			}
		}
	}
}

func TestLocalStore_SearchTruncated_EmptyWhenFewerThanTopKQualify(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "only", Embedding: []float32{1, 0}},
	}

	truncated := store.SearchTruncated([]float32{1, 0}, 0.1, 3, "any.go")

	if len(truncated) != 0 {
		t.Fatalf("expected no truncated candidates when fewer than topK qualify, got %d: %+v", len(truncated), truncated)
	}
}

func TestLocalStore_SearchTruncated_RespectsScopeAndThreshold(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "wrong scope", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "below threshold", Embedding: []float32{0, 1}},
		{Title: "qualifies 1", Embedding: []float32{1, 0}},
		{Title: "qualifies 2", Embedding: []float32{1, 0.1}},
	}

	// Query [1,0], threshold 0.5, topK 1: "wrong scope" filtered by scope,
	// "below threshold" filtered by threshold -- neither should ever appear
	// in SearchTruncated even though topK=1 would otherwise leave room.
	truncated := store.SearchTruncated([]float32{1, 0}, 0.5, 1, "service.go")

	if len(truncated) != 1 {
		t.Fatalf("expected exactly 1 truncated candidate, got %d: %+v", len(truncated), truncated)
	}
	if truncated[0].ADR.Title != "qualifies 2" {
		t.Errorf("expected the lower-scoring qualifying ADR to be the truncated one, got %q", truncated[0].ADR.Title)
	}
}

func TestLocalStore_SearchWithDebugInfo_MatchesIndependentCallsAndIsMutuallyExclusive(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "wrong scope", Scope: ScopePatterns{"**/*.ts"}, Embedding: []float32{1, 0}},
		{Title: "below threshold", Embedding: []float32{0, 1}},
		{Title: "first", Embedding: []float32{1, 0}},
		{Title: "second", Embedding: []float32{1, 0.1}},
		{Title: "third", Embedding: []float32{1, 0.2}},
	}

	hits, rejected, truncated := store.SearchWithDebugInfo([]float32{1, 0}, 0.5, 2, "service.go")

	// "wrong scope" is excluded by scope entirely; "below threshold" scores 0
	// against [1,0] and lands in rejected; the three remaining qualifying
	// ADRs split into 2 hits (topK=2) and 1 truncated.
	if len(hits) != 2 || len(rejected) != 1 || len(truncated) != 1 {
		t.Fatalf("expected 2 hits, 1 rejected, 1 truncated, got hits=%d rejected=%d truncated=%d",
			len(hits), len(rejected), len(truncated))
	}
	if rejected[0].ADR.Title != "below threshold" {
		t.Errorf("expected 'below threshold' to be the rejected candidate, got %q", rejected[0].ADR.Title)
	}
	if truncated[0].ADR.Title != "third" {
		t.Errorf("expected 'third' (lowest qualifying score) to be truncated, got %q", truncated[0].ADR.Title)
	}

	seen := map[string]bool{}
	for _, group := range [][]SearchResult{hits, rejected, truncated} {
		for _, r := range group {
			if seen[r.ADR.Title] {
				t.Errorf("ADR %q appeared in more than one of hits/rejected/truncated", r.ADR.Title)
			}
			seen[r.ADR.Title] = true
		}
	}

	// Independent calls must agree with the consolidated call -- LocalStore
	// is deterministic (no approximate index), so this is guaranteed by
	// construction, but the test pins the invariant explicitly.
	independentHits := store.Search([]float32{1, 0}, 0.5, 2, "service.go")
	independentTruncated := store.SearchTruncated([]float32{1, 0}, 0.5, 2, "service.go")
	if len(independentHits) != len(hits) || len(independentTruncated) != len(truncated) {
		t.Fatalf("SearchWithDebugInfo disagreed with independent Search/SearchTruncated calls")
	}
}

func TestLocalStore_SearchWithDebugInfo_EmptyWhenNoADRs(t *testing.T) {
	store := NewLocalStore(1)

	hits, rejected, truncated := store.SearchWithDebugInfo([]float32{1, 0}, 0.5, 3, "any.go")

	if len(hits) != 0 || len(rejected) != 0 || len(truncated) != 0 {
		t.Fatalf("expected all-empty results for an empty index, got hits=%d rejected=%d truncated=%d", len(hits), len(rejected), len(truncated))
	}
}

func TestLocalStore_ScopedADRs_FiltersByScopeWithoutEmbeddings(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "Go only", Scope: ScopePatterns{"**/*.go"}},
		{Title: "TS only", Scope: ScopePatterns{"**/*.ts"}},
		{Title: "Unscoped"},
	}

	results := store.ScopedADRs("service.go")

	if len(results) != 2 || results[0].ADR.Title != "Go only" || results[1].ADR.Title != "Unscoped" {
		t.Fatalf("expected [Go only, Unscoped], got %+v", results)
	}
	for _, r := range results {
		if r.Score != 0 {
			t.Errorf("expected unscored candidate, got score %v for %q", r.Score, r.ADR.Title)
		}
	}
}
