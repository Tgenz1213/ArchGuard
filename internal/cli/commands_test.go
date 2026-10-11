package cli

import (
	"path/filepath"
	"testing"
)

func TestResolvePaths(t *testing.T) {
	repoRoot := t.TempDir()
	cwd := filepath.Join(repoRoot, "sub")
	elsewhere := filepath.Join(t.TempDir(), "report.txt")

	tests := []struct {
		name     string
		path     string
		wantPath string
	}{
		{"unset stays unset", "", ""},
		{"relative resolves against the starting directory", "report.txt", filepath.Join(cwd, "report.txt")},
		{"parent-relative resolves against the starting directory", filepath.Join("..", "report.txt"), filepath.Join(repoRoot, "report.txt")},
		{"absolute is kept", elsewhere, elsewhere},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := checkCmd{Output: tt.path, configOption: configOption{Config: tt.path}, Paths: []string{"main.go"}}
			index := indexCmd{configOption: configOption{Config: tt.path}}

			cmd.resolvePaths(cwd, repoRoot)
			index.resolvePaths(cwd)

			if cmd.Output != tt.wantPath {
				t.Errorf("Output = %q, want %q", cmd.Output, tt.wantPath)
			}

			if cmd.Config != tt.wantPath {
				t.Errorf("check Config = %q, want %q", cmd.Config, tt.wantPath)
			}

			if index.Config != tt.wantPath {
				t.Errorf("index Config = %q, want %q", index.Config, tt.wantPath)
			}

			if want := "sub/main.go"; cmd.Paths[0] != want {
				t.Errorf("Paths[0] = %q, want %q (relative to the repo root)", cmd.Paths[0], want)
			}
		})
	}
}

func TestInvocationConfigPath(t *testing.T) {
	tests := []struct {
		name string
		inv  invocation
		want string
	}{
		{"check defaults to archguard.yaml", invocation{command: "check"}, configFilename},
		{"index defaults to archguard.yaml", invocation{command: "index"}, configFilename},
		{"check uses --config", invocation{command: "check", check: checkCmd{configOption: configOption{Config: "ci.yaml"}}}, "ci.yaml"},
		{"index uses --config", invocation{command: "index", index: indexCmd{configOption: configOption{Config: "ci.yaml"}}}, "ci.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.inv.configPath(); got != tt.want {
				t.Errorf("configPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
