package stage_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

func cosineStore(adrs ...index.ADR) *index.LocalStore {
	store := index.NewLocalStore(1)
	store.ADRs = adrs
	return store
}

func cosineADR(id string, emb ...float32) index.ADR {
	return index.ADR{ID: id, Title: "ADR " + id, RelPath: id + ".md", Embedding: emb}
}

func queryEmbedder(vec ...float32) llm.Embedder {
	return &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			return vec, nil
		},
	}
}

func candidatesFor(store *index.LocalStore) []stage.Candidate {
	scoped, _ := store.ScopedADRs("svc.go")
	out := make([]stage.Candidate, len(scoped))
	for i, r := range scoped {
		out[i] = stage.Candidate{ADR: r.ADR}
	}
	return out
}

func TestCosineStage_KeepsAboveThresholdBestFirst(t *testing.T) {
	store := cosineStore(cosineADR("far", 0, 1), cosineADR("near", 1, 0), cosineADR("mid", 1, 1))
	s := stage.NewCosineStage(store, queryEmbedder(1, 0), 0.5, 5)

	got, err := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NoDebug, candidatesFor(store))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ids(got) != "near,mid" {
		t.Fatalf("got %s, want near,mid", ids(got))
	}
}

func TestCosineStage_HonorsPerADRThresholdBothWays(t *testing.T) {
	strict, lenient := 0.95, 0.1
	tooStrict := cosineADR("strict", 1, 1)
	tooStrict.SimilarityThreshold = &strict
	lowGlobal := cosineADR("lenient", 1, 1)
	lowGlobal.SimilarityThreshold = &lenient
	store := cosineStore(tooStrict, lowGlobal)
	s := stage.NewCosineStage(store, queryEmbedder(1, 0), 0.9, 5)

	got, _ := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NoDebug, candidatesFor(store))
	if ids(got) != "lenient" {
		t.Fatalf("got %s, want only lenient (0.71 clears its own 0.1, misses strict's 0.95 and the global 0.9)", ids(got))
	}
}

func TestCosineStage_SecondStageSeesOnlyEarlierSurvivors(t *testing.T) {
	store := cosineStore(cosineADR("a", 1, 0), cosineADR("b", 1, 0))
	s := stage.NewCosineStage(store, queryEmbedder(1, 0), 0, 5)
	narrowed := candidatesFor(store)[:1]

	got, _ := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NoDebug, narrowed)
	if ids(got) != "a" {
		t.Fatalf("got %s, want only the candidate it was given, not every ADR the store returns", ids(got))
	}
}

func TestCosineStage_DebugShowsRealScoresForRejectedADRs(t *testing.T) {
	store := cosineStore(cosineADR("near", 1, 0), cosineADR("mid", 1, 1))
	s := stage.NewCosineStage(store, queryEmbedder(1, 0), 0.9, 5)
	var buf bytes.Buffer

	if _, err := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NewDebug(&buf), candidatesFor(store)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Below threshold: ADR mid (score 0.71 < threshold 0.90)") {
		t.Fatalf("debug output %q lacks mid's real cosine score", buf.String())
	}
}

func TestCosineStage_EmbeddingFailureIsReportedAsGeneratingEmbedding(t *testing.T) {
	boom := errors.New("embed down")
	embedder := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			return nil, boom
		},
	}
	store := cosineStore(cosineADR("a", 1, 0))
	s := stage.NewCosineStage(store, embedder, 0, 5)

	_, err := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NoDebug, candidatesFor(store))

	var stageErr *stage.Error
	if !errors.As(err, &stageErr) || stageErr.Action != "generating embedding" || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want a *stage.Error with action generating embedding wrapping the cause", err)
	}
}

func TestCosineStage_OmittedADRsScoreBelowAZeroThreshold(t *testing.T) {
	store := cosineStore(cosineADR("opposite", -1, 0), cosineADR("orthogonal", 0, 1))
	s := stage.NewCosineStage(store, queryEmbedder(1, 0), 0, 5)

	got, _ := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NoDebug, candidatesFor(store))
	if ids(got) != "orthogonal" {
		t.Fatalf("got %s, want only orthogonal: an ADR below a threshold of 0 must not be revived by the unscored value", ids(got))
	}
}

func TestCosineStage_KeepsSameIDDifferentPathADRsApart(t *testing.T) {
	near := cosineADR("dup", 1, 0)
	near.RelPath = "docs/a/dup.md"
	far := cosineADR("dup", 0, 1)
	far.RelPath = "docs/b/dup.md"
	store := cosineStore(near, far)
	s := stage.NewCosineStage(store, queryEmbedder(1, 0), 0.5, 5)

	got, _ := s.Apply(context.Background(), fakeFile{path: "svc.go"}, stage.NoDebug, candidatesFor(store))
	if len(got) != 1 || got[0].ADR.RelPath != "docs/a/dup.md" {
		t.Fatalf("got %d candidates, want only the near ADR: ADRs sharing an ID must keep their own scores", len(got))
	}
}
