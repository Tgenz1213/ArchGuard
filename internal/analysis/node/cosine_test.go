package node_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func cosineStore(adrs ...index.ADR) *index.LocalStore {
	store := index.NewLocalStore(1)
	store.ADRs = adrs
	return store
}

func cosineADR(id string, emb ...float32) index.ADR {
	return index.ADR{ID: id, Title: "ADR " + id, RelPath: id + ".md", Embedding: emb}
}

func queryEmbedder(vec ...float32) inference.Embedder {
	return &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			return vec, nil
		},
	}
}

func candidatesFor(t *testing.T, store *index.LocalStore) []node.Candidate {
	t.Helper()

	scoped, err := store.ScopedADRs("svc.go")
	if err != nil {
		t.Fatalf("ScopedADRs: %v", err)
	}

	out := make([]node.Candidate, len(scoped))
	for i, scopedADR := range scoped {
		out[i] = node.Candidate{ADR: scopedADR.ADR}
	}

	return out
}

func TestCosineNode_KeepsAboveThresholdBestFirst(t *testing.T) {
	store := cosineStore(cosineADR("far", 0, 1), cosineADR("near", 1, 0), cosineADR("mid", 1, 1))
	s := node.NewCosineNode(store, queryEmbedder(1, 0), 0.5, 5)

	got, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, candidatesFor(t, store))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ids(got) != "near,mid" {
		t.Fatalf("got %s, want near,mid", ids(got))
	}
}

func TestCosineNode_HonorsPerADRThresholdBothWays(t *testing.T) {
	strict, lenient := 0.95, 0.1
	tooStrict := cosineADR("strict", 1, 1)
	tooStrict.SimilarityThreshold = &strict
	lowGlobal := cosineADR("lenient", 1, 1)
	lowGlobal.SimilarityThreshold = &lenient
	store := cosineStore(tooStrict, lowGlobal)
	s := node.NewCosineNode(store, queryEmbedder(1, 0), 0.9, 5)

	got, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, candidatesFor(t, store))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "lenient" {
		t.Fatalf("got %s, want only lenient (0.71 clears its own 0.1, misses strict's 0.95 and the global 0.9)", ids(got))
	}
}

func TestCosineNode_SecondNodeSeesOnlyEarlierSurvivors(t *testing.T) {
	store := cosineStore(cosineADR("a", 1, 0), cosineADR("b", 1, 0))
	s := node.NewCosineNode(store, queryEmbedder(1, 0), 0, 5)
	narrowed := candidatesFor(t, store)[:1]

	got, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, narrowed)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "a" {
		t.Fatalf("got %s, want only the candidate it was given, not every ADR the store returns", ids(got))
	}
}

func TestCosineNode_DebugShowsRealScoresForRejectedADRs(t *testing.T) {
	store := cosineStore(cosineADR("near", 1, 0), cosineADR("mid", 1, 1))
	s := node.NewCosineNode(store, queryEmbedder(1, 0), 0.9, 5)
	var buf bytes.Buffer

	if _, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, output.New(&buf, true), candidatesFor(t, store)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "Below threshold: 1 ADR(s), score 0.71 (threshold 0.90)") {
		t.Fatalf("debug output %q lacks mid's real cosine score", buf.String())
	}
}

func TestCosineNode_EmbeddingFailureIsReportedAsGeneratingEmbedding(t *testing.T) {
	boom := errors.New("embed down")
	embedder := &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			return nil, boom
		},
	}
	store := cosineStore(cosineADR("a", 1, 0))
	s := node.NewCosineNode(store, embedder, 0, 5)

	_, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, candidatesFor(t, store))

	var nodeErr *node.Error
	if !errors.As(err, &nodeErr) || nodeErr.Action != "generating embedding" || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want a *node.Error with action generating embedding wrapping the cause", err)
	}

	if nodeErr.Kind != node.KindUnavailable {
		t.Errorf("Kind = %v, want %v", nodeErr.Kind, node.KindUnavailable)
	}
}

func TestCosineNode_MissingEmbedderIsPreconditionNotMet(t *testing.T) {
	store := cosineStore(cosineADR("a", 1, 0))
	s := node.NewCosineNode(store, nil, 0, 5)

	_, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, candidatesFor(t, store))

	var nodeErr *node.Error
	if !errors.As(err, &nodeErr) || nodeErr.Kind != node.KindPreconditionNotMet {
		t.Fatalf("err = %v, want a *node.Error of kind precondition_not_met", err)
	}
}

func TestKind_String(t *testing.T) {
	if got := node.KindUnavailable.String(); got != "unavailable" {
		t.Errorf("KindUnavailable = %q", got)
	}

	if got := node.KindPreconditionNotMet.String(); got != "precondition_not_met" {
		t.Errorf("KindPreconditionNotMet = %q", got)
	}
}

func TestCosineNode_OmittedADRsScoreBelowAZeroThreshold(t *testing.T) {
	store := cosineStore(cosineADR("opposite", -1, 0), cosineADR("orthogonal", 0, 1))
	s := node.NewCosineNode(store, queryEmbedder(1, 0), 0, 5)

	got, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, candidatesFor(t, store))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "orthogonal" {
		t.Fatalf("got %s, want only orthogonal: an ADR below a threshold of 0 must not be revived by the unscored value", ids(got))
	}
}

func TestCosineNode_KeepsSameIDDifferentPathADRsApart(t *testing.T) {
	near := cosineADR("dup", 1, 0)
	near.RelPath = "docs/a/dup.md"
	far := cosineADR("dup", 0, 1)
	far.RelPath = "docs/b/dup.md"
	store := cosineStore(near, far)
	s := node.NewCosineNode(store, queryEmbedder(1, 0), 0.5, 5)

	got, err := s.Apply(t.Context(), fakeFile{path: "svc.go"}, node.NoDebug, candidatesFor(t, store))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(got) != 1 || got[0].ADR.RelPath != "docs/a/dup.md" {
		t.Fatalf("got %d candidates, want only the near ADR: ADRs sharing an ID must keep their own scores", len(got))
	}
}
