//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/baseline"
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

func TestE2E_CheckOutputFlag(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reportE2EConfig)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("text report goes to the file without color, stdout stays empty", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "report.txt")

		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--color", "always", "--output", reportPath, fixtureFilename)
		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout should be empty with --output, got: %q", stdout)
		}

		report := readReportFile(t, reportPath)
		if !strings.Contains(report, fixtureFilename) {
			t.Errorf("report file does not name the violating file:\n%s", report)
		}

		if strings.Contains(report, "\x1b[") {
			t.Errorf("report file contains color escapes:\n%q", report)
		}

		if !strings.Contains(stderr, reportPath) {
			t.Errorf("the log does not say where the report was written:\n%s", stderr)
		}
	})

	t.Run("json report goes to the file, stdout stays empty", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "report.json")

		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--format", "json", "--output", reportPath, fixtureFilename)
		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout should be empty with --output, got: %q", stdout)
		}

		var report checkReport
		if err := json.Unmarshal([]byte(readReportFile(t, reportPath)), &report); err != nil || report.Count != 1 {
			t.Errorf("report file is not the expected JSON document (count 1): %v\n%s", err, readReportFile(t, reportPath))
		}
	})

	t.Run("a relative path resolves against where the command was run", func(t *testing.T) {
		subdir := filepath.Join(tempDir, "sub")
		if err := os.MkdirAll(subdir, 0755); err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}

		_, stderr, exitCode := runCheckWithEnv(t, subdir, binaryPath, nil, "--output", "relative-report.txt", filepath.Join("..", fixtureFilename))
		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
		}

		if _, err := os.Stat(filepath.Join(subdir, "relative-report.txt")); err != nil {
			t.Errorf("report was not written next to where the command ran: %v", err)
		}
	})

	t.Run("a missing directory exits 1 naming the path", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "missing", "report.txt")

		_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--output", reportPath, fixtureFilename)
		if exitCode != int(cli.ExitError) {
			t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitError, stderr)
		}

		if !strings.Contains(stderr, reportPath) {
			t.Errorf("the error does not name the path:\n%s", stderr)
		}

		if _, err := os.Stat(filepath.Dir(reportPath)); err == nil {
			t.Error("the missing directory was created")
		}

		if strings.Contains(stderr, fixtureFilename) {
			t.Errorf("the analysis ran before the destination was rejected:\n%s", stderr)
		}
	})

	t.Run("a directory as the target exits 1 and leaves nothing behind", func(t *testing.T) {
		reportDir := t.TempDir()

		_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--output", reportDir, fixtureFilename)
		if exitCode != int(cli.ExitError) {
			t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitError, stderr)
		}

		if !strings.Contains(stderr, reportDir) {
			t.Errorf("the error does not name the path:\n%s", stderr)
		}

		if entries, err := os.ReadDir(filepath.Dir(reportDir)); err != nil || len(entries) != 1 {
			t.Errorf("expected only the target directory next to it, got %v (err %v)", entries, err)
		}

		if strings.Contains(stderr, fixtureFilename) {
			t.Errorf("the analysis ran before the destination was rejected:\n%s", stderr)
		}
	})
}

func readReportFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	return string(data)
}

func TestE2E_UpdateBaselineEndsWithReport(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reportE2EConfig)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	gitAdd(t, tempDir, fixtureFilename)
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("report on stdout, log on stderr, json ignored", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--update-baseline", "--baseline-reason", "accepted-debt", "--format", "json")
		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitSuccess, stdout, stderr)
		}

		for _, want := range []string{fixtureFilename, "accepted-debt", baseline.Path} {
			if !strings.Contains(stdout, want) {
				t.Errorf("report is missing %q:\n%s", want, stdout)
			}
		}

		if strings.Contains(stdout, "ArchGuard - Architectural Drift Detector") || !strings.Contains(stderr, "ArchGuard - Architectural Drift Detector") {
			t.Errorf("the banner belongs on stderr only.\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if json.Valid([]byte(stdout)) {
			t.Errorf("--format json should be ignored for --update-baseline, but stdout is JSON:\n%s", stdout)
		}
	})

	t.Run("--output writes the report to the file", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "baseline-report.txt")

		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--update-baseline", "--output", reportPath)
		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitSuccess, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout should be empty with --output, got: %q", stdout)
		}

		if report := readReportFile(t, reportPath); !strings.Contains(report, fixtureFilename) || !strings.Contains(report, baseline.Path) {
			t.Errorf("report file is missing the recorded entry or the baseline path:\n%s", report)
		}
	})
}

func TestE2E_CheckOutputRefusesTheBaselineFile(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reportE2EConfig)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	if _, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--update-baseline"); exitCode != int(cli.ExitSuccess) {
		t.Fatalf("creating the baseline failed with exit %d: %s", exitCode, stderr)
	}

	baselinePath := filepath.Join(tempDir, baseline.Path)
	original := readReportFile(t, baselinePath)

	tests := []struct {
		name string
		args []string
	}{
		{"check", []string{"--output", baseline.Path, fixtureFilename}},
		{"check with an absolute path", []string{"--output", baselinePath, fixtureFilename}},
		{"update baseline", []string{"--update-baseline", "--output", baseline.Path}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, tt.args...)
			if exitCode != int(cli.ExitError) {
				t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitError, stderr)
			}

			if strings.Contains(stderr, fixtureFilename) {
				t.Errorf("the analysis ran before the destination was rejected:\n%s", stderr)
			}

			if got := readReportFile(t, baselinePath); got != original {
				t.Errorf("the baseline file was changed:\n%s", got)
			}
		})
	}
}
