package test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

const escape = "\x1b["

func colorTestEnv(extra ...string) []string {
	var env []string

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "TERM":
			continue
		}

		env = append(env, kv)
	}

	return append(append(env, "ARCHGUARD_API_KEY=mock_key"), extra...)
}

func runColorCheck(t *testing.T, dir, binaryPath string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binaryPath, append([]string{"check"}, args...)...)
	cmd.Dir = dir
	cmd.Env = env

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running check: %v", err)
		}

		return outBuf.String(), errBuf.String(), exitErr.ExitCode()
	}

	return outBuf.String(), errBuf.String(), 0
}

func TestE2E_CheckColor(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	plainEnv := colorTestEnv()
	// Warms the analysis cache so the compared runs below see identical cache state.
	runColorCheck(t, tempDir, binaryPath, plainEnv, fixtureFilename)

	t.Run("piped output is plain and matches --color=never", func(t *testing.T) {
		autoOut, autoErr, autoCode := runColorCheck(t, tempDir, binaryPath, plainEnv, fixtureFilename)
		neverOut, neverErr, neverCode := runColorCheck(t, tempDir, binaryPath, plainEnv, fixtureFilename, "--color=never")

		if autoCode != int(cli.ExitDriftDetected) || neverCode != autoCode {
			t.Fatalf("exit codes = %d (auto), %d (never), want %d", autoCode, neverCode, cli.ExitDriftDetected)
		}

		if strings.Contains(autoOut+autoErr, escape) {
			t.Errorf("piped auto output contains an escape sequence:\n%q\n%q", autoOut, autoErr)
		}

		if autoOut != neverOut || autoErr != neverErr {
			t.Errorf("auto and never differ:\nstdout %q vs %q\nstderr %q vs %q", autoOut, neverOut, autoErr, neverErr)
		}
	})

	t.Run("--color=always colors piped output", func(t *testing.T) {
		stdout, _, _ := runColorCheck(t, tempDir, binaryPath, plainEnv, fixtureFilename, "--color=always")

		if !strings.Contains(stdout, escape) {
			t.Errorf("--color=always stdout has no escape sequence:\n%s", stdout)
		}
	})

	t.Run("--color=always wins over NO_COLOR and TERM=dumb", func(t *testing.T) {
		stdout, _, _ := runColorCheck(t, tempDir, binaryPath, colorTestEnv("NO_COLOR=1", "TERM=dumb"), fixtureFilename, "--color=always")

		if !strings.Contains(stdout, escape) {
			t.Errorf("--color=always stdout has no escape sequence:\n%s", stdout)
		}
	})

	t.Run("--format json stays valid JSON with --color=always", func(t *testing.T) {
		stdout, stderr, _ := runColorCheck(t, tempDir, binaryPath, plainEnv, fixtureFilename, "--format=json", "--debug", "--color=always")

		if !json.Valid([]byte(stdout)) || strings.Contains(stdout, escape) {
			t.Errorf("stdout is not plain valid JSON:\n%q", stdout)
		}

		if !strings.Contains(stderr, escape) {
			t.Errorf("stderr has no escape sequence under --color=always:\n%s", stderr)
		}
	})

	t.Run("invalid --color exits with the usage code", func(t *testing.T) {
		_, _, code := runColorCheck(t, tempDir, binaryPath, plainEnv, "--color=rainbow")

		if code != int(cli.ExitUsage) {
			t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
		}
	})
}
