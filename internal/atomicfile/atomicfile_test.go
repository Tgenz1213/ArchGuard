package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_CreatesFileAndCleansUpTmp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")

	if err := Write(path, []byte(`{"key":"value"}`)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != `{"key":"value"}` {
		t.Errorf("expected written content %q, got %q", `{"key":"value"}`, data)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("out.json.tmp was not cleaned up")
	}
}

func TestWrite_CreatesMissingParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "out.json")

	if err := Write(path, []byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist at nested path: %v", err)
	}
}

func TestWrite_RenameFailure_CleansUpTmpFile(t *testing.T) {
	tmpDir := t.TempDir()
	// A directory at the destination makes os.Rename fail without needing OS-specific permission errors.
	path := filepath.Join(tmpDir, "out.json")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("failed to set up destination directory: %v", err)
	}

	if err := Write(path, []byte("data")); err == nil {
		t.Fatal("expected Write to fail when the destination is a directory")
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("out.json.tmp was left behind after a rename failure")
	}
}
