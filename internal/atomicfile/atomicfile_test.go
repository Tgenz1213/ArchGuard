package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWrite_CreatesFileAndCleansUpTmp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")

	if err := Write(path, []byte(`{"key":"value"}`)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	if string(written) != `{"key":"value"}` {
		t.Errorf("expected written content %q, got %q", `{"key":"value"}`, written)
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

func TestWrite_WriteFailure_CleansUpTmpFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "out.json")
	// A directory at path+".tmp" makes os.WriteFile fail without needing OS-specific permission errors.
	if err := os.Mkdir(path+".tmp", 0755); err != nil {
		t.Fatalf("failed to set up tmp-path directory: %v", err)
	}

	if err := Write(path, []byte("data")); err == nil {
		t.Fatal("expected Write to fail when the tmp path is a directory")
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("out.json.tmp was left behind after a write failure")
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

func TestWriteMode_SetsThePermissionBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteMode(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteMode failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}
