package cli

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const retiredPipelineConfig = "analysis:\n  adr_path: docs\n  pipeline:\n    rank:\n      top_k: 2\n"

func answering(answer string) prompter {
	return prompter{scanner: bufio.NewScanner(strings.NewReader(answer))}
}

func TestAddDefaultPipeline_KeepsTheFileModeAndASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and symlinks need a Unix file system")
	}

	dir := t.TempDir()

	target := filepath.Join(dir, "shared.yaml")
	if err := os.WriteFile(target, []byte(retiredPipelineConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "archguard.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := answering("y\n").addDefaultPipeline(link, []byte(retiredPipelineConfig)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("archguard.yaml is no longer a symlink (err %v)", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(got), "- name: shortlist") {
		t.Errorf("target = %q (err %v), want the default pipeline written through the link", got, err)
	}
}

func TestAddDefaultPipeline_ReportsAListPipelineCheckRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archguard.yaml")

	content := []byte("analysis:\n  pipeline: []\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := answering("").addDefaultPipeline(path, content); err == nil {
		t.Fatal("expected an error for a list pipeline check rejects, got nil")
	}

	if got, err := os.ReadFile(path); err != nil || string(got) != string(content) {
		t.Errorf("config = %q (err %v), want it untouched", got, err)
	}
}

func TestVerifySplice(t *testing.T) {
	before := []byte(retiredPipelineConfig)
	tests := []struct {
		name    string
		after   string
		wantErr bool
	}{
		{"only the pipeline changed", "analysis:\n  adr_path: docs\n" + splicedPipeline, false},
		{"another key changed", "analysis:\n  adr_path: elsewhere\n" + splicedPipeline, true},
		{"the new pipeline is invalid", "analysis:\n  adr_path: docs\n  pipeline: []\n", true},
		{"the result doesn't parse", "analysis:\n  adr_path: [docs\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifySplice(before, []byte(tt.after))
			if tt.wantErr && !errors.Is(err, errNotSpliceable) {
				t.Errorf("err = %v, want errNotSpliceable", err)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
