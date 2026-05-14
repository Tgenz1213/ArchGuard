package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const fixtureFilename = "sensitive.js"

func getBinaryName() string {
	if runtime.GOOS == "windows" {
		return "e2e_archguard.exe"
	}
	return "e2e_archguard"
}

// TestE2E_ScanJS verifies that the CLI correctly identifies violations in a JS file
// and passes when the file is removed.
func TestE2E_ScanJS(t *testing.T) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get module root: %v", err)
	}
	sourceRoot := strings.TrimSpace(string(out))

	tempDir := t.TempDir()

	// Initialize a git repo in the temp directory since archguard requires it
	gitInitCmd := exec.Command("git", "init")
	gitInitCmd.Dir = tempDir
	if out, err := gitInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to initialize git in temp dir: %v\nOutput: %s", err, out)
	}

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
	if err := os.WriteFile(filepath.Join(tempDir, "archguard.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create archguard.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create .env: %v", err)
	}

	binaryName := getBinaryName()
	t.Log("Building archguard binary for E2E test...")
	binaryPath := filepath.Join(tempDir, binaryName)
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/archguard-e2e")
	buildCmd.Dir = sourceRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, out)
	}

	fixturePath := filepath.Join(tempDir, fixtureFilename)

	fixtureContent := fmt.Sprintf(`
function sensitiveData() {
    console.log("%s: 123");
}
`, testutil.MockViolationTrigger)

	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	adrPath := filepath.Join(tempDir, "docs", "arch", "0000-no-secrets-in-log.md")
	adrContent := `---
title: "No Secrets in Logs"
status: "Accepted"
scope: "**"
---

## Context
Logging sensitive data is a security risk.

## Decision
Do not print passwords or secrets to console.log.`

	if err := os.MkdirAll(filepath.Dir(adrPath), 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	if err := os.WriteFile(adrPath, []byte(adrContent), 0644); err != nil {
		t.Fatalf("Failed to create mock ADR: %v", err)
	}

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

		runIndexCmd(t, tempDir, binaryPath, int(cli.ExitIndexError))

		// Restore valid config
		err = os.WriteFile(filepath.Join(tempDir, "archguard.yaml"), []byte(configContent), 0644)
		if err != nil {
			t.Fatalf("Failed to restore config: %v", err)
		}
	})

	t.Run("Fails to check with corrupt index", func(t *testing.T) {
		indexPath := filepath.Join(tempDir, ".archguard", "index.json")
		if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
			t.Fatalf("Failed to create archguard dir: %v", err)
		}
		if err := os.WriteFile(indexPath, []byte("{corrupt json"), 0644); err != nil {
			t.Fatalf("Failed to corrupt index: %v", err)
		}

		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitIndexError))

		// Re-run index to fix it for subsequent tests
		runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))
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

// runCheck executes the archguard check command.
func runCheck(t *testing.T, dir, binaryPath, target string, expectedExitCode int) {
	t.Helper()

	const maxRetries = 3
	var lastErr error

	for i := range maxRetries {
		args := []string{"check"}
		if target != "" {
			args = append(args, target)
		}

		cmd := exec.Command(binaryPath, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				t.Fatalf("Binary failed to execute: %v", err)
			}
		}

		if exitCode == expectedExitCode {
			return
		}
		lastErr = fmt.Errorf("expected exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, outputStr)

		if i < maxRetries-1 {
			t.Logf("Retry %d/%d", i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
	}

	t.Fatalf("runCheck failed after %d retries: %v", maxRetries, lastErr)
}

// runIndexCmd executes the archguard index command and checks the expected exit code.
func runIndexCmd(t *testing.T, dir, binaryPath string, expectedExitCode int) {
	t.Helper()

	cmd := exec.Command(binaryPath, "index")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Index binary failed to execute: %v", err)
		}
	}

	if exitCode != expectedExitCode {
		t.Fatalf("expected index exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, outputStr)
	}
	t.Logf("Index output: %s", outputStr)
}
