//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

const configFlagConfig = `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
  pipeline:
    - name: shortlist
      job: rank
      with: cosine
    - name: judge
      job: judge
      with: llm
`

func runArchguard(t *testing.T, dir, binaryPath string, args ...string) (stderr string, exitCode int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return errBuf.String(), exitError.ExitCode()
		}

		t.Fatalf("Binary failed to execute: %v", err)
	}

	return errBuf.String(), 0
}

// The repo has no archguard.yaml, so a run that ignores --config exits 3.
func TestE2E_ConfigFlag(t *testing.T) {
	repo, binaryPath := buildE2EBinary(t)
	writeNoSecretsADR(t, repo)

	configDir := filepath.Join(repo, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(configDir, "ci.yaml"), []byte(configFlagConfig), 0644); err != nil {
		t.Fatalf("Failed to write ci.yaml: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	steps := []struct {
		name     string
		dir      string
		args     []string
		wantCode cli.ExitCode
		wantErr  string
	}{
		{"index reads --config", repo, []string{"index", "--config", "configs/ci.yaml"}, cli.ExitSuccess, ""},
		{"check reads --config", repo, []string{"check", "--config", "configs/ci.yaml", fixtureFilename}, cli.ExitDriftDetected, ""},
		{"relative --config is taken from the starting directory", configDir, []string{"check", "--config", "ci.yaml", "../" + fixtureFilename}, cli.ExitDriftDetected, ""},
		{"absolute --config", repo, []string{"check", "--config", filepath.Join(configDir, "ci.yaml"), fixtureFilename}, cli.ExitDriftDetected, ""},
		{"without --config reads archguard.yaml", repo, []string{"check", fixtureFilename}, cli.ExitConfig, "archguard.yaml"},
		{"missing check --config file", repo, []string{"check", "--config", "missing.yaml", fixtureFilename}, cli.ExitConfig, "missing.yaml"},
		{"missing index --config file", repo, []string{"index", "--config", "missing.yaml"}, cli.ExitConfig, "missing.yaml"},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			stderr, code := runArchguard(t, step.dir, binaryPath, step.args...)
			if code != int(step.wantCode) {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, step.wantCode, stderr)
			}

			if !strings.Contains(stderr, step.wantErr) {
				t.Errorf("stderr does not name %q:\n%s", step.wantErr, stderr)
			}
		})
	}
}
