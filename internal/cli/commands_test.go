package cli

import (
	"path/filepath"
	"testing"
)

func TestCheckCmdResolvePaths(t *testing.T) {
	repoRoot := t.TempDir()
	cwd := filepath.Join(repoRoot, "sub")
	elsewhere := filepath.Join(t.TempDir(), "report.txt")

	tests := []struct {
		name       string
		output     string
		wantOutput string
	}{
		{"unset stays unset", "", ""},
		{"relative resolves against the starting directory", "report.txt", filepath.Join(cwd, "report.txt")},
		{"parent-relative resolves against the starting directory", filepath.Join("..", "report.txt"), filepath.Join(repoRoot, "report.txt")},
		{"absolute is kept", elsewhere, elsewhere},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := checkCmd{Output: tt.output, Paths: []string{"main.go"}}

			cmd.resolvePaths(cwd, repoRoot)

			if cmd.Output != tt.wantOutput {
				t.Errorf("Output = %q, want %q", cmd.Output, tt.wantOutput)
			}

			if want := "sub/main.go"; cmd.Paths[0] != want {
				t.Errorf("Paths[0] = %q, want %q (relative to the repo root)", cmd.Paths[0], want)
			}
		})
	}
}
