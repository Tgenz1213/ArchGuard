package baseline

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveThenLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")
	baseline.Add("adr-002", "file2.go", "")

	if err := baseline.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded == nil {
		t.Fatal("loaded baseline is nil")
		return
	}
	if len(loaded.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(loaded.Entries))
	}

	if loaded.Entries[0].ADRID != "adr-001" || loaded.Entries[0].File != "file1.go" || loaded.Entries[0].QuotedCode != "func main()" {
		t.Errorf("first entry mismatch: %+v", loaded.Entries[0])
	}
	if loaded.Entries[1].ADRID != "adr-002" || loaded.Entries[1].File != "file2.go" || loaded.Entries[1].QuotedCode != "" {
		t.Errorf("second entry mismatch: %+v", loaded.Entries[1])
	}
}

func TestLoad_MissingFile_ReturnsNilNoError(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load should not return error for missing file, got: %v", err)
	}
	if loaded != nil {
		t.Fatalf("Load should return nil for missing file, got: %+v", loaded)
	}
}

func TestLoad_CorruptFile_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupt.json")

	if err := os.WriteFile(path, []byte("{ invalid json"), 0644); err != nil {
		t.Fatalf("Failed to write corrupt file: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load should return error for corrupt JSON")
	}
}

func TestSave_Atomic(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")

	if err := baseline.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("baseline.json was not created")
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("baseline.json.tmp was not cleaned up")
	}
}

func TestSave_RenameFailure_CleansUpTmpFile(t *testing.T) {
	tmpDir := t.TempDir()
	// A directory at the destination path makes os.Rename fail (a file
	// can't be renamed onto an existing directory), simulating any
	// rename failure without depending on OS-specific permission errors.
	path := filepath.Join(tmpDir, "baseline.json")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("failed to set up destination directory: %v", err)
	}

	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")

	if err := baseline.Save(path); err == nil {
		t.Fatal("expected Save to fail when the destination is a directory")
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("baseline.json.tmp was left behind after a rename failure")
	}
}

func TestIsSuppressed_MatchingEntryWithQuotedCodeStillPresent_ReturnsTrue(t *testing.T) {
	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")

	if !baseline.IsSuppressed("adr-001", "file1.go", "package main\n\nfunc main() {\n}") {
		t.Fatal("expected IsSuppressed to return true when quoted code is present")
	}
}

func TestIsSuppressed_QuotedCodeNoLongerPresent_ReturnsFalse(t *testing.T) {
	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")

	if baseline.IsSuppressed("adr-001", "file1.go", "package main\n\nfunc other() {\n}") {
		t.Fatal("expected IsSuppressed to return false when quoted code is not present")
	}
}

func TestIsSuppressed_EmptyQuotedCode_AlwaysSuppressed(t *testing.T) {
	baseline := New()
	baseline.Add("adr-001", "file1.go", "")

	if !baseline.IsSuppressed("adr-001", "file1.go", "package main") {
		t.Fatal("expected IsSuppressed to return true for empty quoted code")
	}

	if !baseline.IsSuppressed("adr-001", "file1.go", "") {
		t.Fatal("expected IsSuppressed to return true for empty quoted code even with empty file content")
	}
}

func TestIsSuppressed_NoMatchingEntry_ReturnsFalse(t *testing.T) {
	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")

	if baseline.IsSuppressed("adr-002", "file1.go", "anything") {
		t.Fatal("expected IsSuppressed to return false for non-matching ADR ID")
	}

	if baseline.IsSuppressed("adr-001", "file2.go", "anything") {
		t.Fatal("expected IsSuppressed to return false for non-matching file")
	}
}

func TestIsSuppressed_NilBaseline_ReturnsFalse(t *testing.T) {
	var baseline *Baseline
	if baseline.IsSuppressed("adr-001", "file1.go", "anything") {
		t.Fatal("expected nil baseline IsSuppressed to return false")
	}
}

func TestAdd_OverwritesExistingEntryForSameKey(t *testing.T) {
	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")
	baseline.Add("adr-001", "file1.go", "func foo()")

	if len(baseline.Entries) != 1 {
		t.Errorf("expected 1 entry after overwrite, got %d", len(baseline.Entries))
	}

	if baseline.Entries[0].QuotedCode != "func foo()" {
		t.Errorf("expected QuotedCode to be 'func foo()', got '%s'", baseline.Entries[0].QuotedCode)
	}
}

func TestSave_UsesCorrectJSONFormat(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	baseline := New()
	baseline.Add("adr-001", "file1.go", "func main()")

	if err := baseline.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	var loaded Baseline
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(loaded.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(loaded.Entries))
	}

	// The file is meant to be human-reviewable in git diffs, so the raw
	// bytes on disk must actually be 2-space-indented, not just
	// unmarshal-compatible.
	var want bytes.Buffer
	if err := json.Indent(&want, data, "", "  "); err != nil {
		t.Fatalf("Failed to compute expected indentation: %v", err)
	}
	if want.String() != string(data) {
		t.Errorf("saved file is not 2-space indented:\ngot:\n%s\nwant:\n%s", data, want.String())
	}
}

func TestSave_SortsEntriesDeterministically(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	baseline := New()
	// Add in a deliberately shuffled (File, ADRID) order.
	baseline.Add("adr-002", "b_file.go", "code b2")
	baseline.Add("adr-001", "a_file.go", "code a1")
	baseline.Add("adr-001", "b_file.go", "code b1")
	baseline.Add("adr-002", "a_file.go", "code a2")

	if err := baseline.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded baseline is nil")
		return
	}

	want := []struct{ File, ADRID string }{
		{"a_file.go", "adr-001"},
		{"a_file.go", "adr-002"},
		{"b_file.go", "adr-001"},
		{"b_file.go", "adr-002"},
	}
	if len(loaded.Entries) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(loaded.Entries))
	}
	for i, w := range want {
		if loaded.Entries[i].File != w.File || loaded.Entries[i].ADRID != w.ADRID {
			t.Errorf("entry %d: got (File=%q, ADRID=%q), want (File=%q, ADRID=%q)",
				i, loaded.Entries[i].File, loaded.Entries[i].ADRID, w.File, w.ADRID)
		}
	}
}
