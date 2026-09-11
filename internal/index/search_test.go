package index

import "testing"

// TestLocalStore_Search_ScopeMatchingADRSurvivesDespiteLowerSimilarity
// reproduces #134: a scope-matching ADR must be evaluated even when 3+
// non-matching-scope ADRs rank higher by similarity than it does.
func TestLocalStore_Search_ScopeMatchingADRSurvivesDespiteLowerSimilarity(t *testing.T) {
	store := NewLocalStore(1)
	store.ADRs = []ADR{
		{Title: "Distractor A", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Distractor B", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Distractor C", Scope: "**/*.ts", Embedding: []float32{1, 0}},
		{Title: "Scope Match", Scope: "**/*.go", Embedding: []float32{1, 1}},
	}

	// Query vector [1,0]: cosine sim with the distractors is 1.0 (identical
	// direction), with "Scope Match" it's ~0.707 -- lower, but still above
	// the 0.5 threshold, and topK=3 is smaller than the 4-ADR corpus, so the
	// pre-fix code (rank-then-filter) would drop "Scope Match" entirely.
	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result (the scope-matching ADR), got %d: %+v", len(results), results)
	}
	if results[0].ADR.Title != "Scope Match" {
		t.Errorf("expected the scope-matching ADR to be returned, got %q", results[0].ADR.Title)
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
