package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

const reportE2EConfig = `
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

func TestE2E_CheckEndsWithReportOnStdout(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reportE2EConfig)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tempDir, "clean.js"), []byte("function ok() { return 1; }\n"), 0644); err != nil {
		t.Fatalf("Failed to create clean fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("violation: report on stdout, log on stderr, no repeated error line", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--debug", fixtureFilename)

		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitDriftDetected, stdout, stderr)
		}

		if !strings.Contains(stdout, fixtureFilename) {
			t.Errorf("report does not name the violating file:\n%s", stdout)
		}

		for _, log := range []string{"ArchGuard - Architectural Drift Detector", "[DEBUG]"} {
			if strings.Contains(stdout, log) {
				t.Errorf("stdout carries log text %q:\n%s", log, stdout)
			}

			if !strings.Contains(stderr, log) {
				t.Errorf("stderr is missing log text %q:\n%s", log, stderr)
			}
		}

		if strings.Contains(stderr, "Error:") {
			t.Errorf("a drift exit repeated the result as an error:\n%s", stderr)
		}
	})

	t.Run("clean run: one line on stdout", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "clean.js")

		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitSuccess, stdout, stderr)
		}

		if lines := strings.Split(strings.TrimSpace(stdout), "\n"); len(lines) != 1 || lines[0] == "" {
			t.Errorf("a run with nothing to report should print one line, got:\n%s", stdout)
		}
	})
}
