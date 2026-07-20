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
	if err := store.BuildIndex(context.Background(), "mock-model", 4, provider, adrProvider); err != nil {
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

func TestLocalStore_BuildIndex_ReturnsErrorOnEmbedFailure(t *testing.T) {
	adrs := []ADR{
		{RelPath: "0001-a.md", Title: "A", Status: "Accepted", Content: "content a"},
		{RelPath: "0002-fails.md", Title: "B", Status: "Accepted", Content: "content b"},
	}
	provider := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string) ([]float32, error) {
			if strings.Contains(text, "Title: B") {
				return nil, fmt.Errorf("simulated embedding failure")
			}
			return []float32{0.1, 0.2}, nil
		},
	}
	adrProvider := &mockADRProvider{adrs: adrs}

	store := NewLocalStore(2)
	err := store.BuildIndex(context.Background(), "mock-model", 2, provider, adrProvider)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "0002-fails.md") {
		t.Errorf("expected error to reference failing ADR path, got: %v", err)
	}
}
