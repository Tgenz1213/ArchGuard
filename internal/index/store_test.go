package index

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/llm"
)

func TestStore_Save_Atomic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "archguard_index_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("Failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

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
	adrs []ADR
	err  error
}

func (m *mockADRProvider) GetADRs(ctx context.Context) ([]ADR, error) {
	return m.adrs, m.err
}

func TestLocalStore_BuildIndex_GeneratesEmbeddings(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
		{RelPath: "0002-b.md", Title: "B", Status: "Accepted", Content: "content b"},
		{RelPath: "0003-c.md", Title: "C", Status: "Accepted", Content: "content c"},
	}
	provider := &llm.MockProvider{EmbeddingDim: 4}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	if _, err := store.BuildIndex(context.Background(), "mock-model", 4, provider, adrProvider); err != nil {
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

// TestLocalStore_BuildIndex_UsesDocumentTaskType asserts BuildIndex embeds
// ADR content with EmbeddingTaskDocument, not EmbeddingTaskQuery.
func TestLocalStore_BuildIndex_UsesDocumentTaskType(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
	}
	var gotTask llm.EmbeddingTaskType
	provider := &llm.MockProvider{
		EmbeddingDim: 4,
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			gotTask = task
			return []float32{0.1, 0.2, 0.3, 0.4}, nil
		},
	}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	if _, err := store.BuildIndex(context.Background(), "mock-model", 4, provider, adrProvider); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	if gotTask != llm.EmbeddingTaskDocument {
		t.Errorf("expected EmbeddingTaskDocument, got %v", gotTask)
	}
}

func TestLocalStore_BuildIndex_SkipsFailedADRAndContinuesEmbeddingOthers(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
		{RelPath: "0002-fails.md", Title: "B", Status: "Accepted", Content: "content b"},
		{RelPath: "0003-c.md", Title: "C", Status: "Accepted", Content: "content c"},
	}
	provider := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "Title: B") {
				return nil, fmt.Errorf("simulated embedding failure")
			}
			return []float32{0.1, 0.2}, nil
		},
	}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	result, err := store.BuildIndex(context.Background(), "mock-model", 2, provider, adrProvider)
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
