//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

const sinceConfig = `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`

func commitAll(t *testing.T, dir, message string) {
	t.Helper()

	for _, args := range [][]string{
		{"add", "."},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", message},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)

		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

func TestE2E_Since_ChecksOnlyWhatChangedOnACleanCheckout(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, sinceConfig)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, "clean.js"), []byte("function ok() { return 1 }\n"), 0o644); err != nil {
		t.Fatalf("write clean.js: %v", err)
	}

	commitAll(t, tempDir, "base")

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0o644); err != nil {
		t.Fatalf("write %s: %v", fixtureFilename, err)
	}

	commitAll(t, tempDir, "add violation")

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	tests := []struct {
		name     string
		args     []string
		wantExit cli.ExitCode
	}{
		{"a clean checkout with no scope checks nothing", []string{"--ci"}, cli.ExitSuccess},
		{"since the base commit finds the violation", []string{"--ci", "--since", "HEAD~1"}, cli.ExitDriftDetected},
		{"nothing changed since HEAD is a clean pass", []string{"--ci", "--since", "HEAD"}, cli.ExitSuccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, code := runCheckWithEnv(t, tempDir, binaryPath, nil, tt.args...)
			if code != int(tt.wantExit) {
				t.Fatalf("exit = %d, want %d", code, tt.wantExit)
			}
		})
	}

	t.Run("an unknown ref is an error, not a pass", func(t *testing.T) {
		_, _, code := runCheckWithEnv(t, tempDir, binaryPath, nil, "--ci", "--since", "no-such-ref")
		if code == int(cli.ExitSuccess) || code == int(cli.ExitDriftDetected) {
			t.Fatalf("exit = %d, want a general error", code)
		}
	})
}
