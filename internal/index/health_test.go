package index

import "testing"

func TestSummarizeCorpus_DetectsDuplicateIDs(t *testing.T) {
	adrs := []ADR{
		{ID: "0001", RelPath: "0001-a.md"},
		{ID: "0001", RelPath: "0001-b.md"},
		{ID: "0002", RelPath: "0002-c.md"},
	}

	summary := summarizeCorpus(adrs, FetchStats{Discovered: 3})

	if len(summary.DuplicateIDs) != 1 {
		t.Fatalf("expected 1 duplicate ID, got %d: %+v", len(summary.DuplicateIDs), summary.DuplicateIDs)
	}
	paths := summary.DuplicateIDs["0001"]
	if len(paths) != 2 || paths[0] != "0001-a.md" || paths[1] != "0001-b.md" {
		t.Errorf("unexpected paths for duplicate ID 0001: %v", paths)
	}
}

func TestSummarizeCorpus_ReportsUnscopedADRs(t *testing.T) {
	adrs := []ADR{
		{ID: "0001", RelPath: "0001-a.md", Scope: ScopePatterns{"**/*.go"}},
		{ID: "0002", RelPath: "0002-b.md"},
	}

	summary := summarizeCorpus(adrs, FetchStats{Discovered: 2})

	if len(summary.NoScope) != 1 || summary.NoScope[0] != "0002-b.md" {
		t.Errorf("expected only 0002-b.md reported as unscoped, got %v", summary.NoScope)
	}
}

func TestSummarizeCorpus_PassesThroughFetchStats(t *testing.T) {
	stats := FetchStats{
		Discovered:     5,
		ParseFailed:    []string{"bad.md"},
		StatusRejected: 2,
	}
	adrs := []ADR{{ID: "0001", RelPath: "0001-a.md"}, {ID: "0002", RelPath: "0002-b.md"}}

	summary := summarizeCorpus(adrs, stats)

	if summary.Discovered != 5 || summary.Valid != 2 || summary.StatusRejected != 2 {
		t.Errorf("unexpected summary counts: %+v", summary)
	}
	if len(summary.ParseFailed) != 1 || summary.ParseFailed[0] != "bad.md" {
		t.Errorf("expected ParseFailed to pass through, got %v", summary.ParseFailed)
	}
}

func TestSummarizeCorpus_NoDuplicatesWhenIDsAreUnique(t *testing.T) {
	adrs := []ADR{
		{ID: "0001", RelPath: "0001-a.md"},
		{ID: "0002", RelPath: "0002-b.md"},
	}

	summary := summarizeCorpus(adrs, FetchStats{Discovered: 2})

	if len(summary.DuplicateIDs) != 0 {
		t.Errorf("expected no duplicates, got %+v", summary.DuplicateIDs)
	}
}

func TestIndexSummary_IsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		summary IndexSummary
		want    bool
	}{
		{"no ADRs discovered at all", IndexSummary{Discovered: 0, Valid: 0}, true},
		{"everything discovered was rejected", IndexSummary{Discovered: 3, Valid: 0}, true},
		{"at least one valid ADR", IndexSummary{Discovered: 3, Valid: 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.summary.IsEmpty(); got != tc.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tc.want)
			}
		})
	}
}
