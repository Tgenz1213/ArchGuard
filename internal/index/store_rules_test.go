package index

import (
	"context"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/tgenz1213/archguard/internal/inference"
)

func TestLocalStore_CalculateHash_UnchangedForADRsWithoutRules(t *testing.T) {
	adrs := []ADR{
		{ID: "0001", RelPath: "0001-a.md", Content: "Body A"},
		{ID: "0002", RelPath: "0002-b.md", Content: "Body B", Rules: Rules{}},
	}

	got, err := NewLocalStore(1).CalculateHash(adrs, "nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}

	if want := "407f15b416f54258dd030bf97df0f5402b9c09ddb4804c721948a3de97644f12"; got != want {
		t.Errorf("hash of a corpus without rules changed: got %s, want %s (existing local indexes would all rebuild)", got, want)
	}
}

func TestLocalStore_CalculateHash_ChangesWhenRulesChange(t *testing.T) {
	store := NewLocalStore(1)
	hash := func(rules Rules) string {
		t.Helper()

		h, err := store.CalculateHash([]ADR{{ID: "0001", RelPath: "0001-a.md", Content: "Body", Rules: rules}}, "model")
		if err != nil {
			t.Fatal(err)
		}

		return h
	}

	none := hash(nil)
	one := hash(statements("A"))
	edited := hash(statements("B"))
	withExample := hash(Rules{{Statement: "A", Violating: []string{"bad()"}}})

	seen := map[string]string{}
	for name, h := range map[string]string{"no rules": none, "rule A": one, "rule B": edited, "rule A with example": withExample} {
		if other, dup := seen[h]; dup {
			t.Errorf("%q and %q hash the same; a rules edit would not trigger a rebuild", name, other)
		}

		seen[h] = name
	}
}

func TestLocalStore_RulesRoundTripThroughSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	rules := Rules{{Statement: "A", Violating: []string{"bad()"}, Compliant: []string{"good()"}}}

	store := NewLocalStore(1)
	store.ModelName, store.Dim, store.Hash = "model", 2, "h"
	store.ADRs = []ADR{{ID: "0001", RelPath: "0001-a.md", Rules: rules}, {ID: "0002", RelPath: "0002-b.md"}}

	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded := NewLocalStore(1)
	if err := loaded.Load(path, "model", 2, "h"); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(loaded.ADRs[0].Rules, rules) || loaded.ADRs[1].Rules != nil {
		t.Errorf("rules after load = %+v / %+v, want %+v / nil", loaded.ADRs[0].Rules, loaded.ADRs[1].Rules, rules)
	}
}

func TestLocalStore_BuildIndex_RulesOnlyEditUpdatesRulesWithoutReembedding(t *testing.T) {
	var embeds atomic.Int32

	provider := &inference.MockProvider{EmbedFunc: func(context.Context, string, inference.EmbeddingTaskType) ([]float32, error) {
		embeds.Add(1)
		return []float32{1, 0}, nil
	}}
	adr := ADR{ID: "0001", RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "Body"}
	store := NewLocalStore(1)

	if _, err := store.BuildIndex(context.Background(), "model", 2, provider, &mockADRProvider{adrs: []ADR{adr}}); err != nil {
		t.Fatal(err)
	}

	hashBefore := store.Hash

	adr.Rules = statements("New rule")
	if _, err := store.BuildIndex(context.Background(), "model", 2, provider, &mockADRProvider{adrs: []ADR{adr}}); err != nil {
		t.Fatal(err)
	}

	if embeds.Load() != 1 {
		t.Errorf("embedding calls = %d, want 1 (a rules-only edit must not re-embed)", embeds.Load())
	}

	if !reflect.DeepEqual(store.ADRs[0].Rules, adr.Rules) {
		t.Errorf("stored rules = %+v, want %+v", store.ADRs[0].Rules, adr.Rules)
	}

	if store.Hash == hashBefore {
		t.Error("stored hash did not change after a rules edit, so check would not see the index as stale")
	}
}
