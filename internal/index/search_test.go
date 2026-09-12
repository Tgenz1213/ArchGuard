package index

import "testing"

// reproduces #134: a lower-similarity scope-matching ADR must still be
// evaluated over 3+ higher-similarity non-matching-scope ADRs.
func TestLocalStore_Search_ScopeMatchingADRSurvivesDespiteLowerSimilarity(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "Distractor A", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Distractor B", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Distractor C", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: "**/*.go", Embedding: []float32{1, 1}},
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
		{Title: "TS only", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "JS only", Scope: "**/*.js", Embedding: []float32{1, 0}},
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
		{Title: "Distractor A", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Distractor B", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Distractor C", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: "**/*.go", Embedding: []float32{0, 1}},
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
		{Title: "Distractor A", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: "**/*.go", Embedding: []float32{1, 1}},
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
		{Title: "wrong scope", Scope: "**/*.ts", Embedding: []float32{1, 1}},
		{Title: "right scope", Scope: "**/*.go", Embedding: []float32{1, 1}},
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
