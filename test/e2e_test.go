package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestMain builds the archguard-e2e binary once and cleans it up after the
// whole package's tests finish -- the binary is stateless, so every test
// sharing one build (instead of each building its own) cuts the package's
// build cost from N `go build` invocations to 1.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedBinaryPath != "" {
		os.RemoveAll(filepath.Dir(sharedBinaryPath))
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
// running inside one) and returns it plus the path to the shared,
// once-built archguard-e2e binary. Each test gets its own repo so tests
// don't interfere with each other.
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

// violationFixtureContent returns JS source containing
// testutil.MockViolationTrigger, tripping the mock chat provider's
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

// runCheckCapture is runCheck but returns output with no retry -- the
// dual-provider suite's mocks are fully deterministic, so a mismatch there
// is a real failure, not flakiness.
func runCheckCapture(t *testing.T, dir, binaryPath, target string, expectedExitCode int) string {
	t.Helper()

	output, exitCode := runCheckOnce(t, dir, binaryPath, target)
	if exitCode != expectedExitCode {
		t.Fatalf("expected check exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, output)
	}
	return output
}
