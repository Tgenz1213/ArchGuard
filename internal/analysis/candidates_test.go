package analysis

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
)

func candidateStore() *index.LocalStore {
	store := index.NewLocalStore(1)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Go only", Scope: index.ScopePatterns{"**/*.go"}},
		{ID: "0002", Title: "TS only", Scope: index.ScopePatterns{"**/*.ts"}},
		{ID: "0003", Title: "Unscoped"},
	}
	return store
}

func mustFor(t *testing.T, src candidateSource, file, content string, debug stage.Debug) []stage.Candidate {
	t.Helper()
	got, err := src.For(file, content, debug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

func candidateIDs(cs []stage.Candidate) string {
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ADR.ID
	}
	return strings.Join(ids, ",")
}

func TestCandidateSource_KeepsOnlyScopeMatchedADRs(t *testing.T) {
	got := mustFor(t, candidateSource{store: candidateStore()}, "svc.go", "package svc", stage.NoDebug)

	if candidateIDs(got) != "0001,0003" {
		t.Fatalf("got %s, want 0001,0003", candidateIDs(got))
	}
	for _, c := range got {
		if c.Score != 0 {
			t.Errorf("candidate %s arrived scored (%v); candidates start unscored", c.ADR.ID, c.Score)
		}
	}
}

func TestCandidateSource_DropsSuppressedADRsAndSaysSo(t *testing.T) {
	var buf bytes.Buffer

	got := mustFor(t, candidateSource{store: candidateStore()}, "svc.go", "// archguard-ignore: 0001\npackage svc", stage.NewDebug(&buf))

	if candidateIDs(got) != "0003" {
		t.Fatalf("got %s, want only 0003", candidateIDs(got))
	}
	if !strings.Contains(buf.String(), "Skipping ADR Go only (Suppressed)") {
		t.Fatalf("debug output %q lacks the suppression line", buf.String())
	}
}

func TestCandidateSource_OnlyReadsTheHeaderForSuppressions(t *testing.T) {
	content := strings.Repeat("x", 2500) + "\n// archguard-ignore: 0001\n"

	got := mustFor(t, candidateSource{store: candidateStore()}, "svc.go", content, stage.NoDebug)

	if candidateIDs(got) != "0001,0003" {
		t.Fatalf("got %s, want a directive past the 2000-byte header to be ignored", candidateIDs(got))
	}
}

type failingScopedStore struct{ index.VectorStore }

func (failingScopedStore) ScopedADRs(string) ([]index.SearchResult, error) {
	return nil, errors.New("db down")
}

func TestCandidateSource_ReturnsStoreErrorsInsteadOfAnEmptyList(t *testing.T) {
	got, err := candidateSource{store: failingScopedStore{}}.For("svc.go", "package svc", stage.NoDebug)

	if err == nil || got != nil {
		t.Fatalf("got %v, %v; want a nil list and the store's error, so a backend failure isn't mistaken for no matching ADRs", got, err)
	}
}
