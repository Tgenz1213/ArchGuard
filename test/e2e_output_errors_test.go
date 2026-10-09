//go:build e2e

package test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

const outputErrorsConfig = `
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

// unwritable returns a read-only handle: every write to it fails, on Windows and Unix alike.
func unwritable(t *testing.T) *os.File {
	t.Helper()

	path := filepath.Join(t.TempDir(), "readonly")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("closing read-only handle: %v", err)
		}
	})

	return f
}

func setupOutputErrorsRepo(t *testing.T) (dir, binaryPath string) {
	t.Helper()

	dir, binaryPath = buildE2EBinary(t)
	writeE2EConfig(t, dir, outputErrorsConfig)
	writeNoSecretsADR(t, dir)

	if err := os.WriteFile(filepath.Join(dir, fixtureFilename), []byte(violationFixtureContent()), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "clean.js"), []byte("console.log('ok');\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir, binaryPath
}

type streams struct{ stdout, stderr *os.File }

func runWithStreams(t *testing.T, dir, binaryPath string, s streams, args ...string) int {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr

	err := cmd.Run()
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("binary failed to run: %v", err)
	}

	return exitErr.ExitCode()
}

func TestE2E_PrimaryOutputWriteFailureExitsOne(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)

	tests := []struct {
		name string
		args []string
	}{
		{"help", []string{"--help"}},
		{"version", []string{"--version"}},
		{"check help", []string{"check", "--help"}},
		{"index summary", []string{"index"}},
		{"clean check result", []string{"check", "clean.js"}},
		{"drift check result wins over drift exit code", []string{"check", fixtureFilename}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args[0] == "check" && tt.args[1] != "--help" {
				runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))
			}

			if code := runWithStreams(t, dir, binaryPath, streams{stdout: unwritable(t), stderr: os.Stderr}, tt.args...); code != int(cli.ExitError) {
				t.Fatalf("exit code = %d, want %d when stdout can't be written", code, cli.ExitError)
			}
		})
	}
}

func TestE2E_JSONReportWriteFailureKeepsTheDriftError(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)
	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	stderrPath := filepath.Join(t.TempDir(), "stderr.txt")

	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}

	code := runWithStreams(t, dir, binaryPath, streams{stdout: unwritable(t), stderr: stderr}, "check", "--format", "json", fixtureFilename)

	if err := stderr.Close(); err != nil {
		t.Fatal(err)
	}

	if code != int(cli.ExitError) {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitError)
	}

	data, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(data, []byte("failed to write json report")) || !bytes.Contains(data, []byte("architectural violations")) {
		t.Errorf("the error should report both the failed write and the drift it hid, got:\n%s", data)
	}
}

func TestE2E_DiagnosticWriteFailureKeepsExitCode(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)
	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	stdoutPath := filepath.Join(t.TempDir(), "stdout.json")

	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}

	code := runWithStreams(t, dir, binaryPath, streams{stdout: stdout, stderr: unwritable(t)}, "check", "--format", "json", "--debug", fixtureFilename)

	if err := stdout.Close(); err != nil {
		t.Fatal(err)
	}

	if code != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code = %d, want %d: a broken stderr must not change the verdict", code, cli.ExitDriftDetected)
	}

	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}

	var report checkReport
	if err := json.Unmarshal(bytes.TrimSpace(data), &report); err != nil {
		t.Fatalf("stdout is not the JSON report: %v\n%s", err, data)
	}

	if report.Count != 1 {
		t.Errorf("report count = %d, want 1", report.Count)
	}
}
