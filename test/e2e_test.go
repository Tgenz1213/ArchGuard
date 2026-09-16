package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const fixtureFilename = "sensitive.js"

const noSecretsADRContent = `---
title: "No Secrets in Logs"
status: "Accepted"
scope: "**"
---

## Context
Logging sensitive data is a security risk.

## Decision
Do not print passwords or secrets to console.log.`

func getBinaryName() string {
	if runtime.GOOS == "windows" {
		return "e2e_archguard.exe"
	}
	return "e2e_archguard"
}

var (
	sharedBinaryOnce sync.Once
	sharedBinaryPath string
	sharedBinaryErr  error
)

// TestMain builds the archguard-e2e binary once, shared by every test.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedBinaryPath != "" {
		if err := os.RemoveAll(filepath.Dir(sharedBinaryPath)); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to clean up shared e2e binary dir: %v\n", err)
		}
	}
	os.Exit(code)
}

func buildSharedE2EBinary(t *testing.T) string {
	t.Helper()

	sharedBinaryOnce.Do(func() {
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			sharedBinaryErr = fmt.Errorf("failed to get module root: %w", err)
			return
		}
		sourceRoot := strings.TrimSpace(string(out))

		binDir, err := os.MkdirTemp("", "archguard-e2e-bin")
		if err != nil {
			sharedBinaryErr = fmt.Errorf("failed to create shared binary dir: %w", err)
			return
		}
		sharedBinaryPath = filepath.Join(binDir, getBinaryName())

		buildCmd := exec.Command("go", "build", "-o", sharedBinaryPath, "./cmd/archguard-e2e")
		buildCmd.Dir = sourceRoot
		if out, err := buildCmd.CombinedOutput(); err != nil {
			sharedBinaryErr = fmt.Errorf("failed to build binary: %w\nOutput: %s", err, out)
		}
	})

	if sharedBinaryErr != nil {
		t.Fatalf("shared E2E binary build failed: %v", sharedBinaryErr)
	}
	return sharedBinaryPath
}

// buildE2EBinary creates an isolated git repo temp dir (archguard requires
// one) and returns it plus the shared archguard-e2e binary's path.
func buildE2EBinary(t *testing.T) (tempDir, binaryPath string) {
	t.Helper()

	tempDir = t.TempDir()

	gitInitCmd := exec.Command("git", "init")
	gitInitCmd.Dir = tempDir
	if out, err := gitInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to initialize git in temp dir: %v\nOutput: %s", err, out)
	}

	return tempDir, buildSharedE2EBinary(t)
}

// writeE2EConfig writes archguard.yaml and an empty .env into dir.
func writeE2EConfig(t *testing.T, dir, configContent string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "archguard.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create archguard.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create .env: %v", err)
	}
}

// writeNoSecretsADR writes the shared "no secrets in logs" ADR fixture.
func writeNoSecretsADR(t *testing.T, dir string) {
	t.Helper()
	adrPath := filepath.Join(dir, "docs", "arch", "0000-no-secrets-in-log.md")
	if err := os.MkdirAll(filepath.Dir(adrPath), 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	if err := os.WriteFile(adrPath, []byte(noSecretsADRContent), 0644); err != nil {
		t.Fatalf("Failed to create mock ADR: %v", err)
	}
}

// violationFixtureContent returns JS source that trips the mock provider's
// violation detection.
func violationFixtureContent() string {
	return fmt.Sprintf(`
function sensitiveData() {
    console.log("%s: 123");
}
`, testutil.MockViolationTrigger)
}

// TestE2E_ScanJS verifies that the CLI correctly identifies violations in a JS file
// and passes when the file is removed.
func TestE2E_ScanJS(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	writeNoSecretsADR(t, tempDir)

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("Unknown command returns usage exit code without config", func(t *testing.T) {
		configPath := filepath.Join(tempDir, "archguard.yaml")
		if err := os.Remove(configPath); err != nil {
			t.Fatalf("Failed to remove config: %v", err)
		}
		defer func() {
			if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
				t.Fatalf("Failed to restore config: %v", err)
			}
		}()

		cmd := exec.Command(binaryPath, "typo")
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		_, err := cmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				t.Fatalf("Binary failed to execute: %v", err)
			}
		}
		if exitCode != int(cli.ExitUsage) {
			t.Fatalf("expected usage exit code %d, got %d", cli.ExitUsage, exitCode)
		}
	})

	t.Run("Help flags exit success at every level", func(t *testing.T) {
		cases := [][]string{
			{"--help"},
			{"-h"},
			{"help"},
			{"check", "--help"},
			{"check", "-h"},
			{"index", "--help"},
			{"index", "-h"},
		}
		for _, args := range cases {
			args := args
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				cmd := exec.Command(binaryPath, args...)
				cmd.Dir = tempDir
				cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

				out, err := cmd.CombinedOutput()
				exitCode := 0
				if err != nil {
					if exitError, ok := err.(*exec.ExitError); ok {
						exitCode = exitError.ExitCode()
					} else {
						t.Fatalf("Binary failed to execute: %v", err)
					}
				}
				if exitCode != int(cli.ExitSuccess) {
					t.Fatalf("expected success exit code %d for %v, got %d (output: %s)", cli.ExitSuccess, args, exitCode, out)
				}
				if !strings.Contains(string(out), "Usage") {
					t.Fatalf("expected usage text in output for %v, got %q", args, out)
				}
			})
		}
	})

	t.Run("Check command invalid flag returns usage exit code", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "check", "--not-a-real-flag")
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		_, err := cmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				t.Fatalf("Binary failed to execute: %v", err)
			}
		}
		if exitCode != int(cli.ExitUsage) {
			t.Fatalf("expected usage exit code %d, got %d", cli.ExitUsage, exitCode)
		}
	})

	t.Run("Index command fails on invalid ADR path", func(t *testing.T) {
		// Change config to point to a non-existent path
		badConfigContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./does_not_exist"
  accepted_statuses: ["Accepted", "Active"]
`
		err := os.WriteFile(filepath.Join(tempDir, "archguard.yaml"), []byte(badConfigContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write bad config: %v", err)
		}
		defer func() {
			if err := os.WriteFile(filepath.Join(tempDir, "archguard.yaml"), []byte(configContent), 0644); err != nil {
				t.Fatalf("Failed to restore config: %v", err)
			}
		}()

		runIndexCmd(t, tempDir, binaryPath, int(cli.ExitIndexError))
	})

	t.Run("Fails to check with corrupt index", func(t *testing.T) {
		indexPath := filepath.Join(tempDir, ".archguard", "index.json")
		if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
			t.Fatalf("Failed to create archguard dir: %v", err)
		}
		if err := os.WriteFile(indexPath, []byte("{corrupt json"), 0644); err != nil {
			t.Fatalf("Failed to corrupt index: %v", err)
		}

		// The index will auto-rebuild because it's corrupt. Then analysis will run and find the violation.
		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))
	})

	t.Run("Detects violation in JS file", func(t *testing.T) {
		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))
	})

	t.Run("Passes after fixture removal", func(t *testing.T) {
		if err := os.Remove(fixturePath); err != nil {
			t.Fatalf("Failed to remove fixture: %v", err)
		}
		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitSuccess))
	})
}

// checkReport mirrors internal/cli's --format json document shape.
type checkReport struct {
	Violations []struct {
		File       string `json:"file"`
		ADRID      string `json:"adr_id"`
		ADRTitle   string `json:"adr_title"`
		Line       int    `json:"line"`
		Reasoning  string `json:"reasoning"`
		QuotedCode string `json:"quoted_code"`
		Suggestion string `json:"suggestion,omitempty"`
	} `json:"violations"`
	Count int `json:"count"`
}

// runCheckJSON runs `archguard check --format json [target]`, capturing
// stdout and stderr separately -- stdout purity is exactly what this
// format exists to guarantee (see issue #70).
func runCheckJSON(t *testing.T, dir, binaryPath, target string) (stdout, stderr string, exitCode int) {
	t.Helper()

	args := []string{"check", "--format", "json"}
	if target != "" {
		args = append(args, target)
	}

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exitError.ExitCode()
		}
		t.Fatalf("Binary failed to execute: %v", err)
	}
	return outBuf.String(), errBuf.String(), 0
}

// TestE2E_CheckFormatJSON verifies --format json: stdout carries only a
// single valid JSON document (no banner, no debug/progress text), the
// violation count matches the exit-code-driving DriftDetectedError.Count,
// and exit codes are unaffected by the flag.
func TestE2E_CheckFormatJSON(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("violations found: valid JSON on stdout only, drift exit code", func(t *testing.T) {
		stdout, _, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)

		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("expected drift exit code %d, got %d. stdout: %s", cli.ExitDriftDetected, exitCode, stdout)
		}

		var report checkReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}
		if report.Count != 1 || len(report.Violations) != 1 {
			t.Fatalf("expected 1 violation, got count=%d len(violations)=%d. stdout: %s", report.Count, len(report.Violations), stdout)
		}
		v := report.Violations[0]
		if v.File != fixtureFilename {
			t.Errorf("expected file %q, got %q", fixtureFilename, v.File)
		}
		if v.ADRID == "" || v.ADRTitle == "" || v.Reasoning == "" {
			t.Errorf("expected populated adr_id/adr_title/reasoning, got %+v", v)
		}
	})

	t.Run("clean run: valid JSON with zero violations, success exit code", func(t *testing.T) {
		if err := os.Remove(fixturePath); err != nil {
			t.Fatalf("Failed to remove fixture: %v", err)
		}
		defer func() {
			if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
				t.Fatalf("Failed to restore fixture: %v", err)
			}
		}()

		stdout, _, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)

		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("expected success exit code %d, got %d. stdout: %s", cli.ExitSuccess, exitCode, stdout)
		}

		var report checkReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}
		if report.Count != 0 || len(report.Violations) != 0 {
			t.Fatalf("expected 0 violations, got count=%d len(violations)=%d. stdout: %s", report.Count, len(report.Violations), stdout)
		}
	})

	t.Run("debug mode routes progress text to stderr, keeping stdout JSON-only", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "check", "--format", "json", "--debug", fixtureFilename)
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		_ = cmd.Run()

		stdout := outBuf.String()
		stderr := errBuf.String()

		var report checkReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON with --debug: %v\nstdout: %q", err, stdout)
		}
		if strings.Contains(stdout, "ArchGuard - Architectural Drift Detector") {
			t.Errorf("banner must not appear on stdout in --format json mode. stdout: %s", stdout)
		}
		if !strings.Contains(stderr, "[DEBUG]") {
			t.Errorf("expected debug output on stderr, got: %s", stderr)
		}
	})

	t.Run("skipped ADR checks: JSON still valid, skip summary lands on stderr", func(t *testing.T) {
		skipFixturePath := filepath.Join(tempDir, "skipped.js")
		skipFixtureContent := fmt.Sprintf(`
function sensitiveData() {
    console.log("%s");
}
`, testutil.MockChatFailureTrigger)
		if err := os.WriteFile(skipFixturePath, []byte(skipFixtureContent), 0644); err != nil {
			t.Fatalf("Failed to create fixture: %v", err)
		}
		defer func() {
			if err := os.Remove(skipFixturePath); err != nil {
				t.Fatalf("Failed to remove fixture: %v", err)
			}
		}()

		stdout, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, "skipped.js")

		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("expected success exit code %d (skips don't drive drift), got %d. stdout: %s stderr: %s", cli.ExitSuccess, exitCode, stdout, stderr)
		}

		var report checkReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}
		if report.Count != 0 {
			t.Fatalf("expected 0 violations for a skipped ADR check, got count=%d. stdout: %s", report.Count, stdout)
		}
		if !strings.Contains(stderr, "1 ADR check(s) were skipped due to LLM errors") {
			t.Errorf("expected the skip-count summary on stderr (not silently dropped by --format json), got: %s", stderr)
		}
	})
}

// TestE2E_SuggestFixes verifies --suggest-fixes: off by default (no second
// LLM call, no suggestion text/field), on when passed (adds the Suggestion
// text line and the JSON suggestion field for a new violation only).
func TestE2E_SuggestFixes(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("off by default: no suggestion in JSON output", func(t *testing.T) {
		stdout, _, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)
		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("expected drift exit code %d, got %d. stdout: %s", cli.ExitDriftDetected, exitCode, stdout)
		}
		var report checkReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}
		if len(report.Violations) != 1 {
			t.Fatalf("expected 1 violation, got %d. stdout: %s", len(report.Violations), stdout)
		}
		if report.Violations[0].Suggestion != "" {
			t.Errorf("expected empty suggestion when --suggest-fixes is not passed, got %q", report.Violations[0].Suggestion)
		}
		if strings.Contains(stdout, `"suggestion"`) {
			t.Errorf("expected the suggestion key to be omitted entirely (omitempty), not just empty, in raw JSON: %s", stdout)
		}
	})

	t.Run("enabled: suggestion appears in JSON output", func(t *testing.T) {
		args := []string{"check", "--format", "json", "--suggest-fixes", fixtureFilename}
		cmd := exec.Command(binaryPath, args...)
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				t.Fatalf("Binary failed to execute: %v", err)
			}
		}
		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("expected drift exit code %d, got %d. stdout: %s stderr: %s", cli.ExitDriftDetected, exitCode, outBuf.String(), errBuf.String())
		}

		var report checkReport
		if err := json.Unmarshal(outBuf.Bytes(), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, outBuf.String())
		}
		if len(report.Violations) != 1 {
			t.Fatalf("expected 1 violation, got %d. stdout: %s", len(report.Violations), outBuf.String())
		}
		want := "Mock suggestion: move this logic into a Go service."
		if report.Violations[0].Suggestion != want {
			t.Errorf("expected suggestion %q, got %q", want, report.Violations[0].Suggestion)
		}
	})

	t.Run("off after a warm cache: no suggestion leaks from the earlier flagged run", func(t *testing.T) {
		stdout, _, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)
		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("expected drift exit code %d, got %d. stdout: %s", cli.ExitDriftDetected, exitCode, stdout)
		}
		var report checkReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}
		if len(report.Violations) != 1 {
			t.Fatalf("expected 1 violation, got %d. stdout: %s", len(report.Violations), stdout)
		}
		if report.Violations[0].Suggestion != "" {
			t.Errorf("expected empty suggestion when --suggest-fixes is not passed, even with a warm cache from the earlier flagged run, got %q", report.Violations[0].Suggestion)
		}
		if strings.Contains(stdout, `"suggestion"`) {
			t.Errorf("expected the suggestion key to be omitted entirely (omitempty), not just empty, in raw JSON: %s", stdout)
		}
	})
}

// TestE2E_CheckFormatJSON_IndexRebuildStaysOffStdout proves an index rebuild
// triggered from inside `check --format json` doesn't leak text onto stdout (#163).
func TestE2E_CheckFormatJSON_IndexRebuildStaysOffStdout(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	adrPath := filepath.Join(tempDir, "docs", "arch", "0000-no-secrets-in-log.md")
	writeNoSecretsADR(t, tempDir)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	// Modifying the ADR after indexing forces a hash mismatch on the next check --
	// deleting index.json instead is covered by TestE2E_CheckRebuildsIndexWhenIndexFileMissing.
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))
	if err := os.WriteFile(adrPath, []byte(noSecretsADRContent+"\n\nUpdated.\n"), 0644); err != nil {
		t.Fatalf("Failed to modify ADR to force a hash mismatch: %v", err)
	}

	stdout, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)

	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("expected drift exit code %d, got %d. stdout: %s stderr: %s", cli.ExitDriftDetected, exitCode, stdout, stderr)
	}

	var report checkReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not valid JSON after triggering an index rebuild: %v\nstdout: %q\nstderr: %q", err, stdout, stderr)
	}
	if report.Count != 1 || len(report.Violations) != 1 {
		t.Fatalf("expected 1 violation, got count=%d len(violations)=%d. stdout: %s", report.Count, len(report.Violations), stdout)
	}

	if !strings.Contains(stderr, "Found 1 valid ADRs") {
		t.Errorf("expected BuildIndex's progress text on stderr (not silently dropped), got: %s", stderr)
	}
	if strings.Contains(stdout, "Found") || strings.Contains(stdout, "Generating embeddings") {
		t.Errorf("BuildIndex progress text leaked onto stdout: %s", stdout)
	}
}

// TestE2E_CheckRebuildsIndexWhenIndexFileMissing regresses issue #174:
// a missing index.json used to silently pass with zero ADRs loaded.
func TestE2E_CheckRebuildsIndexWhenIndexFileMissing(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	indexPath := filepath.Join(tempDir, ".archguard", "index.json")
	if err := os.Remove(indexPath); err != nil {
		t.Fatalf("Failed to delete index.json: %v", err)
	}

	// Before the fix: this silently exited 0 with zero ADRs loaded. After the
	// fix: the missing file triggers a rebuild, and the violation is caught.
	runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))

	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("expected index.json to be rebuilt on disk after check, but it's missing: %v", err)
	}
}

// TestE2E_CheckReportsSkippedADRChecksInsteadOfCleanMessage verifies that an
// LLM-call failure (as opposed to a file/embedding failure) suppresses the
// unqualified "No new architectural violations found." message.
func TestE2E_CheckReportsSkippedADRChecksInsteadOfCleanMessage(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	fixtureContent := fmt.Sprintf(`
function sensitiveData() {
    console.log("%s");
}
`, testutil.MockChatFailureTrigger)
	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	output := runCheckCapture(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitSuccess))

	if strings.Contains(output, "No new architectural violations found.") {
		t.Errorf("check must not print the unqualified clean message when an ADR check was skipped due to an LLM error. Output: %s", output)
	}
	if !strings.Contains(output, "1 ADR check(s) skipped due to LLM errors") {
		t.Errorf("expected the skipped ADR check count to be reported. Output: %s", output)
	}
	if !strings.Contains(output, "compliance was not fully verified") {
		t.Errorf("expected the cli-level skipped-check message to be reported. Output: %s", output)
	}
}

// TestE2E_CheckMultipleFileArgs verifies that `check` analyzes every file
// argument, not just the first (regression test for #132).
func TestE2E_CheckMultipleFileArgs(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)

	fileA := filepath.Join(tempDir, "a.js")
	fileB := filepath.Join(tempDir, "b.js")
	if err := os.WriteFile(fileA, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture a.js: %v", err)
	}
	if err := os.WriteFile(fileB, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture b.js: %v", err)
	}

	writeNoSecretsADR(t, tempDir)

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	// --debug is required for the CLI to print per-file "Analyzing <path>..."
	// lines; the normal violation output has no filename in it at all.
	cmd := exec.Command(binaryPath, "check", "--debug", "a.js", "b.js")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	output := string(out)
	exitCode := 0
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("Binary failed to execute: %v", err)
		}
		exitCode = exitError.ExitCode()
	}

	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("expected exit code %d (drift detected), got %d. Output: %s", cli.ExitDriftDetected, exitCode, output)
	}
	if !strings.Contains(output, "Analyzing a.js") {
		t.Errorf("expected output to mention a.js's violation, got:\n%s", output)
	}
	if !strings.Contains(output, "Analyzing b.js") {
		t.Errorf("expected output to mention b.js's violation, got:\n%s", output)
	}
	if strings.Count(output, "[VIOLATION]") != 2 {
		t.Errorf("expected 2 violations (one per file), got:\n%s", output)
	}
}

// TestE2E_CheckExplicitExcludedFile verifies that an explicitly-named file
// matching exclude_patterns is silently skipped outside --debug, but named
// in a debug-mode skip line (issue #150).
func TestE2E_CheckExplicitExcludedFile(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
  exclude_patterns: ["excluded.js"]
`
	writeE2EConfig(t, tempDir, configContent)

	excludedFile := filepath.Join(tempDir, "excluded.js")
	violatingFile := filepath.Join(tempDir, "b.js")
	if err := os.WriteFile(excludedFile, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture excluded.js: %v", err)
	}
	if err := os.WriteFile(violatingFile, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture b.js: %v", err)
	}

	writeNoSecretsADR(t, tempDir)

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("debug mode names the skipped file", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "check", "--debug", "excluded.js", "b.js")
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		out, err := cmd.CombinedOutput()
		output := string(out)
		exitCode := 0
		if err != nil {
			exitError, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("Binary failed to execute: %v", err)
			}
			exitCode = exitError.ExitCode()
		}

		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("expected exit code %d (drift detected from b.js), got %d. Output: %s", cli.ExitDriftDetected, exitCode, output)
		}
		if !strings.Contains(output, "Skipping excluded.js: explicitly requested but matches exclude_patterns") {
			t.Errorf("expected a debug line naming the skipped excluded file, got:\n%s", output)
		}
		if !strings.Contains(output, "Analyzing b.js") {
			t.Errorf("expected b.js to still be analyzed, got:\n%s", output)
		}
	})

	t.Run("non-debug mode stays silent about the skip", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "check", "excluded.js", "b.js")
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		out, err := cmd.CombinedOutput()
		output := string(out)
		exitCode := 0
		if err != nil {
			exitError, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("Binary failed to execute: %v", err)
			}
			exitCode = exitError.ExitCode()
		}

		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("expected exit code %d (drift detected from b.js), got %d. Output: %s", cli.ExitDriftDetected, exitCode, output)
		}
		if strings.Contains(output, "excluded.js") {
			t.Errorf("expected no mention of the excluded file outside --debug, got:\n%s", output)
		}
	})
}

// alwaysFailsToEmbedADRContent is a valid ADR whose body trips the mock
// provider's embed failure, so it can never be indexed.
var alwaysFailsToEmbedADRContent = fmt.Sprintf(`---
title: "Always Fails To Embed"
status: "Accepted"
scope: "**"
---

## Decision
This ADR is permanently unembeddable: %s`, testutil.MockEmbedFailureTrigger)

// TestE2E_IndexSurvivesPersistentEmbedFailure guards the rebuild loop: a
// permanently unembeddable ADR must not make every `check` exit 5 forever.
func TestE2E_IndexSurvivesPersistentEmbedFailure(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	failingADRPath := filepath.Join(tempDir, "docs", "arch", "0001-always-fails.md")
	if err := os.WriteFile(failingADRPath, []byte(alwaysFailsToEmbedADRContent), 0644); err != nil {
		t.Fatalf("Failed to create failing ADR: %v", err)
	}

	cleanFixture := filepath.Join(tempDir, "clean.js")
	if err := os.WriteFile(cleanFixture, []byte("function greet() {\n    console.log(\"hello\");\n}\n"), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	indexOutput := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))

	if strings.Contains(indexOutput, "ADR Index updated successfully") {
		t.Errorf("index must not claim unqualified success when an ADR was skipped. Output: %s", indexOutput)
	}
	if !strings.Contains(indexOutput, "Failed to embed or persist: 1") {
		t.Errorf("expected the skipped ADR to be reported. Output: %s", indexOutput)
	}
	if !strings.Contains(indexOutput, "0001-always-fails.md") {
		t.Errorf("expected the skipped ADR to be named. Output: %s", indexOutput)
	}

	// Two separate invocations: pre-fix, the saved hash excluded the skipped
	// ADR while check hashed the full set, so every run rebuilt and exited 5.
	for i := 1; i <= 2; i++ {
		output, exitCode := runCheckOnce(t, tempDir, binaryPath, "clean.js")
		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("check run %d: expected exit code %d, got %d. Output: %s", i, cli.ExitSuccess, exitCode, output)
		}
		if strings.Contains(output, "failed to load rebuilt index") {
			t.Fatalf("check run %d hit the rebuild loop. Output: %s", i, output)
		}
	}
}

// TestE2E_IndexPrintsSummaryWhenAllADRsFailToEmbed verifies the summary
// still prints when BuildIndex errors (every ADR failed to embed).
func TestE2E_IndexPrintsSummaryWhenAllADRsFailToEmbed(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(adrDir, "0001-always-fails.md"), []byte(alwaysFailsToEmbedADRContent), 0644); err != nil {
		t.Fatalf("Failed to write ADR: %v", err)
	}

	output, exitCode := runIndexOnce(t, tempDir, binaryPath)
	if exitCode != int(cli.ExitIndexError) {
		t.Fatalf("expected exit code %d, got %d. Output: %s", cli.ExitIndexError, exitCode, output)
	}
	if !strings.Contains(output, "1 discovered, 0 valid") {
		t.Errorf("expected the summary to print even though BuildIndex returned an error. Output: %s", output)
	}
	if !strings.Contains(output, "Failed to embed or persist: 1") {
		t.Errorf("expected the failed ADR to be reported in the summary. Output: %s", output)
	}
}

// TestE2E_IndexReportsFullCorpusHealthSummary exercises a valid ADR, a parse
// failure, a status rejection, and a duplicate ID together, in one corpus.
func TestE2E_IndexReportsFullCorpusHealthSummary(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}

	valid := "---\ntitle: \"Valid\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent"
	rejected := "---\ntitle: \"Draft\"\nstatus: \"Proposed\"\nscope: \"**\"\n---\nContent"
	unparseable := "not frontmatter at all"
	dup1 := "---\ntitle: \"Dup A\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent"
	dup2 := "---\ntitle: \"Dup B\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent"

	files := map[string]string{
		"0001-valid.md":       valid,
		"0002-rejected.md":    rejected,
		"0003-unparseable.md": unparseable,
		"0004-first.md":       dup1,
		"0004-second.md":      dup2,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(adrDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))

	if !strings.Contains(output, "5 discovered, 3 valid") {
		t.Errorf("expected 5 discovered, 3 valid. Output: %s", output)
	}
	if !strings.Contains(output, "Skipped (parse failure): 1") || !strings.Contains(output, "0003-unparseable.md") {
		t.Errorf("expected the parse failure to be named. Output: %s", output)
	}
	if !strings.Contains(output, "Skipped (status not accepted): 1") {
		t.Errorf("expected the status rejection to be counted. Output: %s", output)
	}
	if !strings.Contains(output, "Duplicate ADR IDs: 1") || !strings.Contains(output, "0004-first.md") || !strings.Contains(output, "0004-second.md") {
		t.Errorf("expected the duplicate ID collision to be reported. Output: %s", output)
	}
}

// TestE2E_IndexReportsDuplicateADRIDs verifies the summary flags two ADRs
// sharing an ID, since a duplicate breaks archguard-ignore/baseline scoping.
func TestE2E_IndexReportsDuplicateADRIDs(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	// Both files parse their ADR ID from the leading "-"-delimited filename
	// segment, so these two collide on ID "0001" despite different paths.
	dup1 := "---\ntitle: \"First\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent A"
	dup2 := "---\ntitle: \"Second\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent B"
	if err := os.WriteFile(filepath.Join(adrDir, "0001-first.md"), []byte(dup1), 0644); err != nil {
		t.Fatalf("Failed to write first ADR: %v", err)
	}
	if err := os.WriteFile(filepath.Join(adrDir, "0001-second.md"), []byte(dup2), 0644); err != nil {
		t.Fatalf("Failed to write second ADR: %v", err)
	}

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))

	if !strings.Contains(output, "Duplicate ADR IDs: 1") {
		t.Errorf("expected duplicate ADR IDs to be reported. Output: %s", output)
	}
	if !strings.Contains(output, "0001-first.md") || !strings.Contains(output, "0001-second.md") {
		t.Errorf("expected both colliding paths to be named. Output: %s", output)
	}
}

// Verifies analysis.adr_id_pattern is threaded through cli.Execute to LocalProvider.SetIDPattern.
func TestE2E_IndexADRIDPatternAvoidsCollision(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
  adr_id_pattern: "^adr-(\\d+)-"
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	// Both files collapse to ID "adr" under the default first-hyphen split.
	dup1 := "---\ntitle: \"First\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent A"
	dup2 := "---\ntitle: \"Second\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent B"
	if err := os.WriteFile(filepath.Join(adrDir, "adr-1-first.md"), []byte(dup1), 0644); err != nil {
		t.Fatalf("Failed to write first ADR: %v", err)
	}
	if err := os.WriteFile(filepath.Join(adrDir, "adr-2-second.md"), []byte(dup2), 0644); err != nil {
		t.Fatalf("Failed to write second ADR: %v", err)
	}

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))

	if !strings.Contains(output, "2 discovered, 2 valid") {
		t.Errorf("expected 2 discovered, 2 valid. Output: %s", output)
	}
	if strings.Contains(output, "Duplicate ADR IDs") {
		t.Errorf("expected no duplicate ADR IDs once adr_id_pattern distinguishes them. Output: %s", output)
	}
}

// TestE2E_IndexNoValidADRsExitsWithError verifies a corpus where every
// discovered ADR was excluded (accepted_statuses) exits non-zero, not success.
func TestE2E_IndexNoValidADRsExitsWithError(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	rejected := "---\ntitle: \"Draft Only\"\nstatus: \"Proposed\"\nscope: \"**\"\n---\nNot yet accepted."
	if err := os.WriteFile(filepath.Join(adrDir, "0001-draft.md"), []byte(rejected), 0644); err != nil {
		t.Fatalf("Failed to write ADR: %v", err)
	}

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitIndexError))

	if !strings.Contains(output, "1 discovered, 0 valid") {
		t.Errorf("expected the summary to show 1 discovered, 0 valid. Output: %s", output)
	}
}

// TestE2E_IndexEmptyADRDirectoryExitsWithError verifies discovering nothing
// (e.g. a misconfigured adr_path) fails loudly rather than reporting success.
func TestE2E_IndexEmptyADRDirectoryExitsWithError(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitIndexError))

	if !strings.Contains(output, "0 discovered, 0 valid") {
		t.Errorf("expected the summary to show 0 discovered, 0 valid. Output: %s", output)
	}
}

// TestE2E_IndexFailedRebuildLeavesPriorLocalIndexUntouched verifies a failed
// empty-corpus rebuild doesn't overwrite a previously-healthy local index.
func TestE2E_IndexFailedRebuildLeavesPriorLocalIndexUntouched(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`
	writeE2EConfig(t, tempDir, configContent)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	adrPath := filepath.Join(adrDir, "0001-valid.md")
	valid := "---\ntitle: \"Valid\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\nContent"
	if err := os.WriteFile(adrPath, []byte(valid), 0644); err != nil {
		t.Fatalf("Failed to write ADR: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	indexPath := filepath.Join(tempDir, ".archguard", "index.json")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("Failed to read index.json after the healthy build: %v", err)
	}

	// Flip the ADR's status so this run discovers it but rejects it, forcing
	// Valid to 0 without deleting or renaming the file.
	rejected := "---\ntitle: \"Valid\"\nstatus: \"Proposed\"\nscope: \"**\"\n---\nContent"
	if err := os.WriteFile(adrPath, []byte(rejected), 0644); err != nil {
		t.Fatalf("Failed to rewrite ADR: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitIndexError))

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("Failed to read index.json after the failed rebuild: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("expected index.json to be left untouched by the failed rebuild.\nBefore: %s\nAfter: %s", before, after)
	}
}

// gitAdd stages path so AllProvider's `git ls-files` sees it.
func gitAdd(t *testing.T, dir, path string) {
	t.Helper()

	cmd := exec.Command("git", "add", "--", path)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to git add %s: %v\nOutput: %s", path, err, out)
	}
}

// TestE2E_BaselineMode verifies the full lifecycle: write, suppress, re-surface.
func TestE2E_BaselineMode(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	violatingLine := fmt.Sprintf(`console.log("%s: 123");`, testutil.MockViolationTrigger)
	fixtureContent := fmt.Sprintf(`
function sensitiveData() {
    %s
}
`, violatingLine)
	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}
	// --update-baseline scans via `git ls-files`, so the fixture must be tracked.
	gitAdd(t, tempDir, fixtureFilename)

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	var writtenEntry baseline.Entry
	t.Run("update-baseline writes baseline file", func(t *testing.T) {
		cmd := exec.Command(binaryPath, "check", "--update-baseline")
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		out, err := cmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				t.Fatalf("Binary failed to execute: %v", err)
			}
		}
		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("expected exit code %d, got %d. Output: %s", cli.ExitSuccess, exitCode, out)
		}

		baselinePath := filepath.Join(tempDir, baseline.Path)
		data, err := os.ReadFile(baselinePath)
		if err != nil {
			t.Fatalf("Failed to read baseline file %s: %v", baselinePath, err)
		}

		var b baseline.Baseline
		if err := json.Unmarshal(data, &b); err != nil {
			t.Fatalf("Failed to unmarshal baseline file: %v\nContent: %s", err, data)
		}

		if len(b.Entries) != 1 {
			t.Fatalf("expected exactly 1 baseline entry, got %d: %+v", len(b.Entries), b.Entries)
		}
		writtenEntry = b.Entries[0]

		const expectedADRID = "0000" // from writeNoSecretsADR's "0000-no-secrets-in-log.md"
		if writtenEntry.ADRID != expectedADRID {
			t.Errorf("expected baseline entry ADRID %q, got %q", expectedADRID, writtenEntry.ADRID)
		}
		if writtenEntry.File != fixtureFilename {
			t.Errorf("expected baseline entry File %q, got %q", fixtureFilename, writtenEntry.File)
		}
		if writtenEntry.QuotedCode != violatingLine {
			t.Errorf("expected baseline entry QuotedCode %q, got %q", violatingLine, writtenEntry.QuotedCode)
		}
	})

	t.Run("check suppresses baselined violation", func(t *testing.T) {
		output := runCheckCapture(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitSuccess))
		if !strings.Contains(output, "baselined") {
			t.Errorf("expected output to mention baselined suppression, got: %s", output)
		}
	})

	t.Run("check re-flags violation once baselined line changes", func(t *testing.T) {
		newViolatingLine := fmt.Sprintf(`console.log("%s: 456");`, testutil.MockViolationTrigger)
		newFixtureContent := fmt.Sprintf(`
function sensitiveData() {
    %s
}
`, newViolatingLine)
		if err := os.WriteFile(fixturePath, []byte(newFixtureContent), 0644); err != nil {
			t.Fatalf("Failed to update fixture: %v", err)
		}

		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))
	})
}

// TestE2E_BaselineMode_SaveFailureDoesNotPrintSuccess asserts the success
// message never prints if the baseline file write itself fails.
func TestE2E_BaselineMode_SaveFailureDoesNotPrintSuccess(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	// Renaming onto a non-empty directory fails on both Linux and Windows,
	// forcing Save to fail without a flaky permissions trick.
	obstructionDir := filepath.Join(tempDir, baseline.Path)
	if err := os.MkdirAll(obstructionDir, 0755); err != nil {
		t.Fatalf("Failed to create obstruction directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(obstructionDir, "placeholder"), []byte("x"), 0644); err != nil {
		t.Fatalf("Failed to create obstruction placeholder file: %v", err)
	}

	cmd := exec.Command(binaryPath, "check", "--update-baseline")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Binary failed to execute: %v", err)
		}
	}

	if exitCode != int(cli.ExitError) {
		t.Fatalf("expected exit code %d (Save failure), got %d. Output: %s", cli.ExitError, exitCode, out)
	}

	output := string(out)
	if strings.Contains(output, "Baseline scan complete") {
		t.Errorf("success message printed despite Save failure. Output: %s", output)
	}
	if !strings.Contains(output, "failed to write baseline file") {
		t.Errorf("expected the actual Save failure to be reported. Output: %s", output)
	}
}

// TestE2E_BaselineMode_BaselineReasonFlagIsRecorded asserts --baseline-reason
// is written onto every entry --update-baseline collects in that run.
func TestE2E_BaselineMode_BaselineReasonFlagIsRecorded(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixtureFilename := "reason_fixture.js"
	fixturePath := filepath.Join(tempDir, fixtureFilename)
	violatingLine := fmt.Sprintf(`console.log("%s: 123");`, testutil.MockViolationTrigger)
	fixtureContent := fmt.Sprintf(`
function sensitiveData() {
    %s
}
`, violatingLine)
	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}
	gitAdd(t, tempDir, fixtureFilename)

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	cmd := exec.Command(binaryPath, "check", "--update-baseline", "--baseline-reason", "accepted-debt")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Binary failed to execute: %v", err)
		}
	}
	if exitCode != int(cli.ExitSuccess) {
		t.Fatalf("expected exit code %d, got %d. Output: %s", cli.ExitSuccess, exitCode, out)
	}

	baselinePath := filepath.Join(tempDir, baseline.Path)
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("Failed to read baseline file %s: %v", baselinePath, err)
	}

	var b baseline.Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("Failed to unmarshal baseline file: %v\nContent: %s", err, data)
	}
	if len(b.Entries) != 1 {
		t.Fatalf("expected exactly 1 baseline entry, got %d: %+v", len(b.Entries), b.Entries)
	}
	if b.Entries[0].Reason != "accepted-debt" {
		t.Errorf("expected Reason %q, got %q", "accepted-debt", b.Entries[0].Reason)
	}
}

// TestE2E_BaselineMode_UpdateBaselineRecoversFromCorruptBaselineFile asserts
// --update-baseline still succeeds when the existing baseline file is
// corrupt, since it's the documented recovery path for that situation.
func TestE2E_BaselineMode_UpdateBaselineRecoversFromCorruptBaselineFile(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
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
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	fixtureFilename := "corrupt_recovery_fixture.js"
	fixturePath := filepath.Join(tempDir, fixtureFilename)
	violatingLine := fmt.Sprintf(`console.log("%s: 123");`, testutil.MockViolationTrigger)
	fixtureContent := fmt.Sprintf(`
function sensitiveData() {
    %s
}
`, violatingLine)
	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}
	gitAdd(t, tempDir, fixtureFilename)

	baselinePath := filepath.Join(tempDir, baseline.Path)
	if err := os.WriteFile(baselinePath, []byte("{ not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write corrupt baseline fixture: %v", err)
	}

	t.Log("Indexing ADRs for E2E test...")
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	cmd := exec.Command(binaryPath, "check", "--update-baseline")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Binary failed to execute: %v", err)
		}
	}
	if exitCode != int(cli.ExitSuccess) {
		t.Fatalf("expected --update-baseline to recover from a corrupt baseline file with exit code %d, got %d. Output: %s", cli.ExitSuccess, exitCode, out)
	}
	if !strings.Contains(string(out), "baseline reasons will not carry forward") {
		t.Errorf("expected output to warn that baseline reasons will not carry forward, got: %s", out)
	}

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("Failed to read regenerated baseline file %s: %v", baselinePath, err)
	}
	var b baseline.Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("Regenerated baseline file is not valid JSON: %v\nContent: %s", err, data)
	}
	if len(b.Entries) != 1 {
		t.Fatalf("expected exactly 1 regenerated baseline entry, got %d: %+v", len(b.Entries), b.Entries)
	}
}

// runIndexOnce executes `archguard index` once and returns its output
// alongside the exit code actually observed.
func runIndexOnce(t *testing.T, dir, binaryPath string) (output string, exitCode int) {
	t.Helper()

	cmd := exec.Command(binaryPath, "index")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	outputStr := string(out)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return outputStr, exitError.ExitCode()
		}
		t.Fatalf("Index binary failed to execute: %v", err)
	}
	return outputStr, 0
}

// runIndexCmd runs archguard index and fails the test if the exit code doesn't match.
func runIndexCmd(t *testing.T, dir, binaryPath string, expectedExitCode int) {
	t.Helper()

	output, exitCode := runIndexOnce(t, dir, binaryPath)
	if exitCode != expectedExitCode {
		t.Fatalf("expected index exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, output)
	}
	t.Logf("Index output: %s", output)
}

// runIndexCmdCapture is runIndexCmd but returns the output for assertions
// beyond the exit code.
func runIndexCmdCapture(t *testing.T, dir, binaryPath string, expectedExitCode int) string {
	t.Helper()

	output, exitCode := runIndexOnce(t, dir, binaryPath)
	if exitCode != expectedExitCode {
		t.Fatalf("expected index exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, output)
	}
	return output
}

// runCheckOnce executes `archguard check [target]` once and returns its
// output alongside the exit code actually observed.
func runCheckOnce(t *testing.T, dir, binaryPath, target string) (output string, exitCode int) {
	t.Helper()

	args := []string{"check"}
	if target != "" {
		args = append(args, target)
	}

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	outputStr := string(out)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return outputStr, exitError.ExitCode()
		}
		t.Fatalf("Binary failed to execute: %v", err)
	}
	return outputStr, 0
}

// runCheck executes archguard check, retrying up to 3 times on exit-code mismatch.
func runCheck(t *testing.T, dir, binaryPath, target string, expectedExitCode int) {
	t.Helper()

	const maxRetries = 3
	var lastErr error

	for i := range maxRetries {
		output, exitCode := runCheckOnce(t, dir, binaryPath, target)
		if exitCode == expectedExitCode {
			return
		}
		lastErr = fmt.Errorf("expected exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, output)

		if i < maxRetries-1 {
			t.Logf("Retry %d/%d", i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
	}

	t.Fatalf("runCheck failed after %d retries: %v", maxRetries, lastErr)
}

// runCheckCapture is runCheck but with no retry -- callers are deterministic.
func runCheckCapture(t *testing.T, dir, binaryPath, target string, expectedExitCode int) string {
	t.Helper()

	output, exitCode := runCheckOnce(t, dir, binaryPath, target)
	if exitCode != expectedExitCode {
		t.Fatalf("expected check exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, output)
	}
	return output
}
