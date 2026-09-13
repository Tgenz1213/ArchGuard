package baseline

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveThenLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	baseline := New()
	baseline.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()"})
	baseline.Add(Entry{ADRID: "adr-002", File: "file2.go", QuotedCode: ""})

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
	baseline.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()"})

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
	baseline.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()"})

	if err := baseline.Save(path); err == nil {
		t.Fatal("expected Save to fail when the destination is a directory")
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("baseline.json.tmp was left behind after a rename failure")
	}
}

func newBaselineWithEntry(adrID, file, quotedCode string) *Baseline {
	b := New()
	b.Add(Entry{ADRID: adrID, File: file, QuotedCode: quotedCode})
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
	baseline.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()"})
	baseline.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func foo()"})

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
	baseline.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()"})

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
	baseline.Add(Entry{ADRID: "adr-002", File: "b_file.go", QuotedCode: "code b2"})
	baseline.Add(Entry{ADRID: "adr-001", File: "a_file.go", QuotedCode: "code a1"})
	baseline.Add(Entry{ADRID: "adr-001", File: "b_file.go", QuotedCode: "code b1"})
	baseline.Add(Entry{ADRID: "adr-002", File: "a_file.go", QuotedCode: "code a2"})

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

func TestEntry_ReasonField_RoundTripsThroughSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	b := New()
	b.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()", Reason: "accepted-debt"})

	if err := b.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil || len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 loaded entry, got %+v", loaded)
	}
	if loaded.Entries[0].Reason != "accepted-debt" {
		t.Errorf("expected Reason %q, got %q", "accepted-debt", loaded.Entries[0].Reason)
	}
}

func TestEntry_ReasonField_OmittedWhenEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	b := New()
	b.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()"})

	if err := b.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	if strings.Contains(string(data), `"reason"`) {
		t.Errorf("expected no \"reason\" key in JSON when Reason is empty, got:\n%s", data)
	}
}

func TestLoad_PreReasonFieldBaselineFile_LoadsWithEmptyReason(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "baseline.json")

	legacyJSON := `{
  "entries": [
    {
      "adr_id": "adr-001",
      "file": "file1.go",
      "quoted_code": "func main()"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy baseline fixture: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed on pre-reason-field file: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Reason != "" {
		t.Errorf("expected empty Reason for legacy entry, got %q", loaded.Entries[0].Reason)
	}
	if loaded.Entries[0].QuotedCode != "func main()" {
		t.Errorf("expected QuotedCode to still load correctly, got %q", loaded.Entries[0].QuotedCode)
	}
}

func TestAdd_OverwriteReplacesReasonToo(t *testing.T) {
	b := New()
	b.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()", Reason: "accepted-debt"})
	b.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func foo()"})

	if len(b.Entries) != 1 {
		t.Fatalf("expected 1 entry after overwrite, got %d", len(b.Entries))
	}
	if b.Entries[0].Reason != "" {
		t.Errorf("expected overwrite to replace Reason with the new (empty) value, got %q", b.Entries[0].Reason)
	}
}

func TestReasonFor(t *testing.T) {
	tests := []struct {
		name     string
		baseline *Baseline
		adrID    string
		file     string
		want     string
	}{
		{
			name:     "matching entry with no reason set",
			baseline: newBaselineWithEntry("adr-001", "file1.go", "func main()"),
			adrID:    "adr-001",
			file:     "file1.go",
			want:     "",
		},
		{
			name: "matching entry with an explicit reason",
			baseline: func() *Baseline {
				b := New()
				b.Add(Entry{ADRID: "adr-001", File: "file1.go", QuotedCode: "func main()", Reason: "false-positive"})
				return b
			}(),
			adrID: "adr-001",
			file:  "file1.go",
			want:  "false-positive",
		},
		{
			name:     "non-matching ADR ID returns empty",
			baseline: newBaselineWithEntry("adr-001", "file1.go", "func main()"),
			adrID:    "adr-002",
			file:     "file1.go",
			want:     "",
		},
		{
			name:     "nil baseline returns empty",
			baseline: nil,
			adrID:    "adr-001",
			file:     "file1.go",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.baseline.ReasonFor(tt.adrID, tt.file); got != tt.want {
				t.Errorf("ReasonFor() = %q, want %q", got, tt.want)
			}
		})
	}
}
