package index

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestLocalStore_CalculateHash_ChangesWhenIDChanges(t *testing.T) {
	s := NewLocalStore(1)

	adrsBefore := []ADR{{ID: "0001", RelPath: "0001-foo.md", Content: "Body"}}
	adrsAfter := []ADR{{ID: "1", RelPath: "0001-foo.md", Content: "Body"}}

	hashBefore, err := s.CalculateHash(adrsBefore, "model")
	if err != nil {
		t.Fatalf("CalculateHash failed: %v", err)
	}

	hashAfter, err := s.CalculateHash(adrsAfter, "model")
	if err != nil {
		t.Fatalf("CalculateHash failed: %v", err)
	}

	if hashBefore == hashAfter {
		t.Errorf("expected hash to change when ADR.ID changes with identical RelPath/Content, got same hash %q for both", hashBefore)
	}
}

func TestStore_Save_Atomic(t *testing.T) {
	tmpDir := t.TempDir()

	store := NewLocalStore(5)
	store.ModelName = "mock-model"
	store.Dim = 128
	store.Hash = "test-hash"

	indexPath := filepath.Join(tmpDir, "index.json")

	store.ADRs = []ADR{
		{
			Title:     "Test ADR",
			Status:    "Accepted",
			Content:   "Content",
			Embedding: []float32{0.1, 0.2, 0.3},
		},
	}

	if err := store.Save(indexPath); err != nil {
		t.Fatalf("Store.Save failed: %v", err)
	}

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Fatalf("index.json was not created")
	}

	if _, err := os.Stat(indexPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("index.json.tmp was not cleaned up")
	}

	loadedStore := NewLocalStore(5)

	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("Failed to read index.json: %v", err)
	}

	if err := json.Unmarshal(data, loadedStore); err != nil {
		t.Fatalf("Failed to unmarshal saved index: %v", err)
	}

	if loadedStore.ModelName != store.ModelName {
		t.Errorf("Expected ModelName %s, got %s", store.ModelName, loadedStore.ModelName)
	}

	if len(loadedStore.ADRs) != 1 {
		t.Errorf("Expected 1 ADR, got %d", len(loadedStore.ADRs))
	}
}

func TestStore_Save_RenameFailure_CleansUpTmpFile(t *testing.T) {
	tmpDir := t.TempDir()
	// A directory at the destination makes os.Rename fail without needing OS-specific permission errors.
	path := filepath.Join(tmpDir, "index.json")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("failed to set up destination directory: %v", err)
	}

	store := NewLocalStore(5)
	store.ModelName = "mock-model"

	if err := store.Save(path); err == nil {
		t.Fatal("expected Save to fail when the destination is a directory")
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("index.json.tmp was left behind after a rename failure")
	}
}

type mockADRProvider struct {
	adrs  []ADR
	stats FetchStats
	err   error
}

func (m *mockADRProvider) GetADRs(ctx context.Context) ([]ADR, FetchStats, error) {
	return m.adrs, m.stats, m.err
}

func TestLocalStore_BuildIndex_GeneratesEmbeddings(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
		{RelPath: "0002-b.md", Title: "B", Status: "Accepted", Content: "content b"},
		{RelPath: "0003-c.md", Title: "C", Status: "Accepted", Content: "content c"},
	}
	provider := &inference.MockProvider{EmbeddingDim: 4}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	if _, err := store.BuildIndex(t.Context(), "mock-model", 4, provider, adrProvider); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if len(store.ADRs) != 3 {
		t.Fatalf("expected 3 ADRs, got %d", len(store.ADRs))
	}

	for _, adr := range store.ADRs {
		if len(adr.Embedding) != 4 {
			t.Errorf("ADR %s: expected embedding of length 4, got %d", adr.RelPath, len(adr.Embedding))
		}
	}
}

func TestLocalStore_BuildIndex_UsesDocumentTaskType(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
	}
	var gotTask inference.EmbeddingTaskType
	provider := &inference.MockProvider{
		EmbeddingDim: 4,
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			gotTask = task
			return []float32{0.1, 0.2, 0.3, 0.4}, nil
		},
	}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	if _, err := store.BuildIndex(t.Context(), "mock-model", 4, provider, adrProvider); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if gotTask != inference.EmbeddingTaskDocument {
		t.Errorf("expected EmbeddingTaskDocument, got %v", gotTask)
	}
}

func TestLocalStore_BuildIndex_SkipsFailedADRAndContinuesEmbeddingOthers(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
		{RelPath: "0002-fails.md", Title: "B", Status: "Accepted", Content: "content b"},
		{RelPath: "0003-c.md", Title: "C", Status: "Accepted", Content: "content c"},
	}
	provider := &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "Title: B") {
				return nil, fmt.Errorf("simulated embedding failure")
			}

			return []float32{0.1, 0.2}, nil
		},
	}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)

	result, err := store.BuildIndex(t.Context(), "mock-model", 2, provider, adrProvider)
	if err != nil {
		t.Fatalf("BuildIndex must not return an error for a single ADR embed failure, got: %v", err)
	}

	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped ADR, got %d: %+v", len(result.Skipped), result.Skipped)
	}

	if result.Skipped[0].RelPath != "0002-fails.md" {
		t.Errorf("expected skipped ADR to be 0002-fails.md, got %s", result.Skipped[0].RelPath)
	}

	if !strings.Contains(result.Skipped[0].Err.Error(), "simulated embedding failure") {
		t.Errorf("expected skipped ADR error to reference the underlying failure, got: %v", result.Skipped[0].Err)
	}

	if result.Valid != 2 {
		t.Errorf("expected Valid to count only successfully-indexed ADRs (2), got %d", result.Valid)
	}

	if len(store.ADRs) != 2 {
		t.Fatalf("expected 2 ADRs to remain in the corpus (the failed one excluded), got %d", len(store.ADRs))
	}

	for _, adr := range store.ADRs {
		if adr.RelPath == "0002-fails.md" {
			t.Errorf("failed ADR 0002-fails.md must be excluded from the corpus, but it is present")
		}

		if len(adr.Embedding) == 0 {
			t.Errorf("ADR %s: expected a non-empty embedding, got none", adr.RelPath)
		}
	}
}

// Attempted lets a caller tell "fetch never happened" apart from "fetch
// happened and found nothing," which look identical from zero counts alone.
func TestLocalStore_BuildIndex_AttemptedFalseWhenFetchFails(t *testing.T) {
	provider := &inference.MockProvider{EmbeddingDim: 2}
	adrProvider := &mockADRProvider{err: fmt.Errorf("boom")}

	store := NewLocalStore(2)

	result, err := store.BuildIndex(t.Context(), "mock-model", 2, provider, adrProvider)
	if err == nil {
		t.Fatal("expected an error when the ADR provider fails")
	}

	if result.Attempted {
		t.Error("expected Attempted to be false when GetADRs itself failed")
	}
}

func TestLocalStore_BuildIndex_AttemptedTrueOnSuccess(t *testing.T) {
	adrs := []ADR{{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"}}
	provider := &inference.MockProvider{EmbeddingDim: 2}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)

	result, err := store.BuildIndex(t.Context(), "mock-model", 2, provider, adrProvider)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if !result.Attempted {
		t.Error("expected Attempted to be true once GetADRs succeeded")
	}
}

// A canceled ctx must fail the build even when every ADR is unchanged and nothing is embedded.
func TestLocalStore_BuildIndex_DetectsCancelledContextOnNoEmbedRun(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
	}
	provider := &inference.MockProvider{EmbeddingDim: 2}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	if _, err := store.BuildIndex(t.Context(), "mock-model", 2, provider, adrProvider); err != nil {
		t.Fatalf("initial BuildIndex failed: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := store.BuildIndex(ctx, "mock-model", 2, provider, adrProvider)
	if err == nil {
		t.Fatal("expected an error from a canceled context on a no-embed run, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected the error to wrap context.Canceled, got: %v", err)
	}
}

func TestLocalStore_BuildIndex_PreservesSimilarityThresholdOverride(t *testing.T) {
	override := 0.6
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a", SimilarityThreshold: &override},
		{RelPath: "0002-b.md", Title: "B", Status: "Accepted", Content: "content b"},
	}
	provider := &inference.MockProvider{EmbeddingDim: 4}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	if _, err := store.BuildIndex(t.Context(), "mock-model", 4, provider, adrProvider); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	byPath := make(map[string]ADR)
	for _, adr := range store.ADRs {
		byPath[adr.RelPath] = adr
	}

	if got := byPath["0001-a.md"].SimilarityThreshold; got == nil || *got != 0.6 {
		t.Errorf("expected 0001-a.md to keep its similarity_threshold override, got %v", got)
	}

	if got := byPath["0002-b.md"].SimilarityThreshold; got != nil {
		t.Errorf("expected 0002-b.md to have no override, got %v", *got)
	}
}

func TestLocalStore_BuildIndex_WritesProgressToConfiguredWriter(t *testing.T) {
	dir := t.TempDir()
	writeADRFile(t, dir, "0001-a.md", "---\ntitle: A\nstatus: Accepted\n---\nBody")

	provider := NewLocalProvider(dir, []string{"Accepted"})
	embedProvider := &inference.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			return []float32{0.1, 0.2}, nil
		},
	}

	var buf bytes.Buffer
	store := NewLocalStore(1)
	store.out = output.New(&buf, false)

	_, err := store.BuildIndex(t.Context(), "model", 2, embedProvider, provider)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if !strings.Contains(buf.String(), "Found 1 valid ADRs") {
		t.Errorf("expected progress text on the configured writer, got %q", buf.String())
	}
}

func TestLocalStore_Load_MissingFileReturnsError(t *testing.T) {
	tmpDir := t.TempDir()

	s := NewLocalStore(1)
	missingPath := filepath.Join(tmpDir, "does-not-exist.json")

	err := s.Load(missingPath, "model", 768, "somehash")
	if err == nil {
		t.Fatal("expected Load to return a non-nil error when the index file does not exist, got nil")
	}

	if len(s.ADRs) != 0 {
		t.Fatalf("expected ADRs to remain empty on a missing-file Load, got %d", len(s.ADRs))
	}
}

func TestLocalStore_BuildIndex_ReusesSavedEmbeddings(t *testing.T) {
	savedADRs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
		{RelPath: "0002-b.md", Title: "B", Status: "Accepted", Content: "content b"},
		{RelPath: "0003-c.md", Title: "C", Status: "Accepted", Content: "content c"},
	}

	editedB := slices.Clone(savedADRs)
	editedB[1].Content = "content b, edited"

	tests := []struct {
		name         string
		adrs         []ADR
		model        string
		dim          int
		wantEmbedded []string
	}{
		{name: "nothing changed", adrs: savedADRs, model: "model-a", dim: 4},
		{name: "one ADR changed", adrs: editedB, model: "model-a", dim: 4, wantEmbedded: []string{"B"}},
		{name: "model changed", adrs: savedADRs, model: "model-b", dim: 4, wantEmbedded: []string{"A", "B", "C"}},
		{name: "dimension changed", adrs: savedADRs, model: "model-a", dim: 8, wantEmbedded: []string{"A", "B", "C"}},
		{name: "dimension unset", adrs: savedADRs, model: "model-a", dim: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			indexPath := filepath.Join(t.TempDir(), "index.json")

			saved := NewLocalStore(1)
			if _, err := saved.BuildIndex(t.Context(), "model-a", 4, &inference.MockProvider{EmbeddingDim: 4}, &mockADRProvider{adrs: slices.Clone(savedADRs)}); err != nil {
				t.Fatalf("initial BuildIndex: %v", err)
			}

			if err := saved.Save(indexPath); err != nil {
				t.Fatalf("Save: %v", err)
			}

			embedder := &recordingEmbedder{dim: tc.dim}

			store := NewLocalStore(1)
			if err := store.LoadSaved(indexPath); err != nil {
				t.Fatalf("LoadSaved: %v", err)
			}

			if _, err := store.BuildIndex(t.Context(), tc.model, tc.dim, embedder, &mockADRProvider{adrs: slices.Clone(tc.adrs)}); err != nil {
				t.Fatalf("BuildIndex: %v", err)
			}

			if got := embedder.embeddedTitles(); !slices.Equal(got, tc.wantEmbedded) {
				t.Errorf("embedded %v, want %v", got, tc.wantEmbedded)
			}

			for _, adr := range store.ADRs {
				if wantLen := cmp.Or(tc.dim, 4); len(adr.Embedding) != wantLen {
					t.Errorf("ADR %s: embedding length %d, want %d", adr.RelPath, len(adr.Embedding), wantLen)
				}
			}
		})
	}
}

func TestLocalStore_LoadSaved_MissingFileLeavesStoreEmpty(t *testing.T) {
	store := NewLocalStore(1)
	if err := store.LoadSaved(filepath.Join(t.TempDir(), "missing.json")); err != nil {
		t.Fatalf("LoadSaved on a missing file: %v", err)
	}

	if len(store.ADRs) != 0 {
		t.Errorf("expected no ADRs, got %d", len(store.ADRs))
	}
}

func TestLocalStore_LoadSaved_CorruptFileEmbedsEveryADR(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(indexPath, []byte(`{"adrs": [{"rel_path": 1}], "model_name": "model-a", "dim": 4}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewLocalStore(1)
	if err := store.LoadSaved(indexPath); err == nil {
		t.Fatal("expected an error for a corrupt index")
	}

	adrs := []ADR{{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"}}
	embedder := &recordingEmbedder{dim: 4}

	if _, err := store.BuildIndex(t.Context(), "model-a", 4, embedder, &mockADRProvider{adrs: adrs}); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	if got := embedder.embeddedTitles(); len(got) != 1 {
		t.Errorf("embedded %v, want the one ADR", got)
	}
}

type recordingEmbedder struct {
	dim    int
	mu     sync.Mutex
	titles []string
}

func (e *recordingEmbedder) CreateEmbedding(_ context.Context, text string, _ inference.EmbeddingTaskType) (inference.Embedding, error) {
	title, _, _ := strings.Cut(strings.TrimPrefix(text, "Title: "), "\n")

	e.mu.Lock()
	e.titles = append(e.titles, title)
	e.mu.Unlock()

	v := make([]float32, cmp.Or(e.dim, 4))
	v[0] = 1

	return inference.Embedding{Vector: v}, nil
}

func (e *recordingEmbedder) embeddedTitles() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return slices.Sorted(slices.Values(e.titles))
}

func TestLocalStore_BuildIndex_ShortensAnADRTheModelRejectsAsTooLong(t *testing.T) {
	adrs := []ADR{{RelPath: "0001-long.md", Title: "Long", Status: "Accepted", Content: strings.Repeat("a long decision\n", 100)}}
	embedder := &inference.MockProvider{EmbedFunc: func(_ context.Context, text string, _ inference.EmbeddingTaskType) ([]float32, error) {
		if len(text) > 500 {
			return nil, inference.ErrInputTooLong
		}

		return []float32{1, 0}, nil
	}}

	var out bytes.Buffer
	store := NewLocalStore(1)
	store.out = output.New(&out, false)

	result, err := store.BuildIndex(t.Context(), "model", 2, embedder, &mockADRProvider{adrs: adrs})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	if len(result.Skipped) != 0 || len(store.ADRs) != 1 || len(store.ADRs[0].Embedding) != 2 {
		t.Fatalf("expected the long ADR indexed, got skipped %v and %d ADRs", result.Skipped, len(store.ADRs))
	}

	if !strings.Contains(out.String(), "0001-long.md") {
		t.Errorf("expected a note naming the shortened ADR, got %q", out.String())
	}
}

func TestLocalStore_Load_NamesWhyTheIndexIsStale(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "index.json")

	saved := &LocalStore{ADRs: []ADR{}, Hash: "hash-a", ModelName: "model-a", Dim: 4}
	if err := saved.Save(indexPath); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		model   string
		dim     int
		hash    string
		want    []error
		notWant []error
	}{
		{name: "matches", path: indexPath, model: "model-a", dim: 4, hash: "hash-a", notWant: []error{ErrIndexMissing, ErrEmbeddingModelChanged, ErrADRsChanged}},
		{name: "missing", path: indexPath + ".gone", model: "model-a", dim: 4, hash: "hash-a", want: []error{ErrIndexMissing}},
		{name: "ADRs changed", path: indexPath, model: "model-a", dim: 4, hash: "hash-b", want: []error{ErrADRsChanged}, notWant: []error{ErrEmbeddingModelChanged}},
		{name: "dimension changed", path: indexPath, model: "model-a", dim: 8, hash: "hash-a", want: []error{ErrEmbeddingModelChanged}, notWant: []error{ErrADRsChanged}},
		{name: "model changed hides the hash it also changed", path: indexPath, model: "model-b", dim: 4, hash: "hash-b", want: []error{ErrEmbeddingModelChanged}, notWant: []error{ErrADRsChanged}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewLocalStore(1).Load(tc.path, tc.model, tc.dim, tc.hash)

			if len(tc.want) == 0 && err != nil {
				t.Fatalf("Load: %v", err)
			}

			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Errorf("err = %v, want it to be %v", err, want)
				}
			}

			for _, notWant := range tc.notWant {
				if errors.Is(err, notWant) {
					t.Errorf("err = %v, want it not to be %v", err, notWant)
				}
			}
		})
	}
}
