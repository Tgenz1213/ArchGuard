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
	// A directory at the destination makes os.Rename fail without needing OS-specific permission errors.
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

func newBaselineWithEntry(adrID, file, quotedCode string) *Baseline {
	b := New()
	b.Add(adrID, file, quotedCode)
	return b
}

func TestIsSuppressed(t *testing.T) {
	tests := []struct {
		name           string
		baseline       *Baseline
		adrID          string
		file           string
		currentContent string
		want           bool
	}{
		{
			name:           "matching entry with quoted code still present",
			baseline:       newBaselineWithEntry("adr-001", "file1.go", "func main()"),
			adrID:          "adr-001",
			file:           "file1.go",
			currentContent: "package main\n\nfunc main() {\n}",
			want:           true,
		},
		{
			name:           "quoted code no longer present",
			baseline:       newBaselineWithEntry("adr-001", "file1.go", "func main()"),
			adrID:          "adr-001",
			file:           "file1.go",
			currentContent: "package main\n\nfunc other() {\n}",
			want:           false,
		},
		{
			name:           "empty quoted code is always suppressed",
			baseline:       newBaselineWithEntry("adr-001", "file1.go", ""),
			adrID:          "adr-001",
			file:           "file1.go",
			currentContent: "package main",
			want:           true,
		},
		{
			name:           "empty quoted code is always suppressed even with empty file content",
			baseline:       newBaselineWithEntry("adr-001", "file1.go", ""),
			adrID:          "adr-001",
			file:           "file1.go",
			currentContent: "",
			want:           true,
		},
		{
			name:           "non-matching ADR ID",
			baseline:       newBaselineWithEntry("adr-001", "file1.go", "func main()"),
			adrID:          "adr-002",
			file:           "file1.go",
			currentContent: "anything",
			want:           false,
		},
		{
			name:           "non-matching file",
			baseline:       newBaselineWithEntry("adr-001", "file1.go", "func main()"),
			adrID:          "adr-001",
			file:           "file2.go",
			currentContent: "anything",
			want:           false,
		},
		{
			name:           "nil baseline",
			baseline:       nil,
			adrID:          "adr-001",
			file:           "file1.go",
			currentContent: "anything",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.baseline.IsSuppressed(tt.adrID, tt.file, tt.currentContent); got != tt.want {
				t.Errorf("IsSuppressed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSave_NilBaseline_ReturnsNilError(t *testing.T) {
	var baseline *Baseline
	path := filepath.Join(t.TempDir(), "archguard-baseline.json")

	if err := baseline.Save(path); err != nil {
		t.Fatalf("expected nil baseline Save to return nil, got: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file to be written for a nil baseline, got err: %v", err)
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

	// Must be human-reviewable in git diffs: 2-space-indented on disk, not just unmarshal-compatible.
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
