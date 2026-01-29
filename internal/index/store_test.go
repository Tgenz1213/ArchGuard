package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_Save_Atomic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "archguard_index_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	indexPath := filepath.Join(tmpDir, "index.json")
	store := NewStore()
	store.ModelName = "test-model"
	store.Dim = 128
	store.Hash = "test-hash"

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

	loadedStore := NewStore()
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
