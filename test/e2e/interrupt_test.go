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

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const interruptConfig = `
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

func setupInterruptRepo(t *testing.T) (dir, binaryPath string) {
	t.Helper()

	dir, binaryPath = buildE2EBinary(t)
	writeE2EConfig(t, dir, interruptConfig)
	writeNoSecretsADR(t, dir)

	source := "function f() {\n    return \"" + testutil.MockInterruptTrigger + "\";\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "interrupt.js"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	gitAdd(t, dir, "interrupt.js")
	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	return dir, binaryPath
}

func runInterrupted(t *testing.T, dir, binaryPath string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("binary failed to execute: %v", err)
	}

	return outBuf.String(), errBuf.String(), cmd.ProcessState.ExitCode()
}

func TestE2E_InterruptedCheckExits130(t *testing.T) {
	dir, binaryPath := setupInterruptRepo(t)

	stdout, stderr, exitCode := runInterrupted(t, dir, binaryPath, "check", "interrupt.js")

	if exitCode != int(cli.ExitInterrupted) {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", exitCode, cli.ExitInterrupted, stdout, stderr)
	}

	if !strings.Contains(stderr, "Error: interrupted") {
		t.Errorf("stderr = %q, want it to report the interruption", stderr)
	}

	if strings.Contains(stdout, "No new architectural violations found") {
		t.Errorf("an interrupted run must not report a clean result, got stdout: %s", stdout)
	}
}

func TestE2E_InterruptedJSONCheckPrintsNoReport(t *testing.T) {
	dir, binaryPath := setupInterruptRepo(t)

	stdout, stderr, exitCode := runInterrupted(t, dir, binaryPath, "check", "--format", "json", "interrupt.js")

	if exitCode != int(cli.ExitInterrupted) {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", exitCode, cli.ExitInterrupted, stderr)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want no partial JSON report", stdout)
	}
}

func TestE2E_InterruptedIndexExits130AndWritesNoIndex(t *testing.T) {
	dir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, dir, interruptConfig)
	writeNoSecretsADR(t, dir)

	// A second, healthy ADR means a build that ignored the cancel would still succeed.
	adr := "---\ntitle: \"Interrupts\"\nstatus: \"Accepted\"\n---\n\n## Decision\n" + testutil.MockInterruptTrigger + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docs", "arch", "0001-interrupts.md"), []byte(adr), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runInterrupted(t, dir, binaryPath, "index")

	if exitCode != int(cli.ExitInterrupted) {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", exitCode, cli.ExitInterrupted, stdout, stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, ".archguard", "index.json")); !os.IsNotExist(err) {
		t.Errorf("an interrupted index must not write .archguard/index.json (stat err: %v)", err)
	}
}

func TestE2E_InterruptedUpdateBaselineKeepsExistingBaseline(t *testing.T) {
	dir, binaryPath := setupInterruptRepo(t)

	const prior = `{"entries":[{"adr_id":"0000","file":"old.js","quoted_code":"keep me"}]}`

	baselinePath := filepath.Join(dir, baseline.Path)
	if err := os.WriteFile(baselinePath, []byte(prior), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runInterrupted(t, dir, binaryPath, "check", "--update-baseline")

	if exitCode != int(cli.ExitInterrupted) {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", exitCode, cli.ExitInterrupted, stdout, stderr)
	}

	got, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != prior {
		t.Errorf("baseline was rewritten by an interrupted run:\n%s", got)
	}
}
