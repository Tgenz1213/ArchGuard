package index

import "testing"

func adrWithScope(title, scope string) *ADR {
	return &ADR{Title: title, Scope: scope}
}

func TestFilterByScope(t *testing.T) {
	candidates := []SearchResult{
		{ADR: adrWithScope("no scope", ""), Score: 0.5},
		{ADR: adrWithScope("matching scope", "**/*.go"), Score: 0.4},
		{ADR: adrWithScope("non-matching scope", "**/*.ts"), Score: 0.9},
	}

	got := filterByScope(candidates, "service.go")

	if len(got) != 2 {
		t.Fatalf("expected 2 candidates to survive, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.ADR.Title == "non-matching scope" {
			t.Errorf("candidate with non-matching scope should have been filtered out, got %+v", c)
		}
	}
}

func TestFilterByScope_EmptyScopeAlwaysMatches(t *testing.T) {
	candidates := []SearchResult{
		{ADR: adrWithScope("global ADR", ""), Score: 0.1},
	}

	got := filterByScope(candidates, "anything/at/all.rb")

	if len(got) != 1 {
		t.Fatalf("expected the scopeless ADR to survive for any file, got %d results", len(got))
	}
}

func TestRankAndLimit_SortsDescendingAndCuts(t *testing.T) {
	candidates := []SearchResult{
		{ADR: adrWithScope("low", ""), Score: 0.2},
		{ADR: adrWithScope("high", ""), Score: 0.9},
		{ADR: adrWithScope("mid", ""), Score: 0.5},
	}

	got := rankAndLimit(candidates, 2)

	if len(got) != 2 {
		t.Fatalf("expected topK=2 results, got %d", len(got))
	}
	if got[0].ADR.Title != "high" || got[1].ADR.Title != "mid" {
		t.Errorf("expected [high, mid] in descending-score order, got [%s, %s]", got[0].ADR.Title, got[1].ADR.Title)
	}
}

func TestRankAndLimit_FewerThanTopKReturnsAll(t *testing.T) {
	candidates := []SearchResult{
		{ADR: adrWithScope("only", ""), Score: 0.5},
	}

	got := rankAndLimit(candidates, 5)

	if len(got) != 1 {
		t.Fatalf("expected 1 result when candidates < topK, got %d", len(got))
	}
}

func TestFilterByThreshold(t *testing.T) {
	candidates := []SearchResult{
		{ADR: adrWithScope("above", ""), Score: 0.8},
		{ADR: adrWithScope("at threshold", ""), Score: 0.5},
		{ADR: adrWithScope("below", ""), Score: 0.2},
	}

	got := filterByThreshold(candidates, 0.5)

	if len(got) != 2 {
		t.Fatalf("expected 2 candidates at or above threshold, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.ADR.Title == "below" {
			t.Errorf("candidate below threshold should have been filtered out, got %+v", c)
		}
	}
}

func TestFilterByThreshold_EmptyInput(t *testing.T) {
	got := filterByThreshold(nil, 0.5)
	if len(got) != 0 {
		t.Fatalf("expected no results from empty input, got %d", len(got))
	}
}
