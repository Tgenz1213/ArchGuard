package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	fixtureContent = `
function sensitiveData() {
    console.log("password: 123");
}
`
	fixtureFilename = "sensitive.js"
	binaryName      = "e2e_archguard.exe"
)

// TestE2E_ScanJS verifies that the CLI correctly identifies violations in a JS file
// and passes when the file is removed.
func TestE2E_ScanJS(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	sourceRoot := filepath.Dir(wd)
	if filepath.Base(wd) != "test" {
		sourceRoot = wd
	}

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

	t.Log("Building archguard binary for E2E test...")
	binaryPath := filepath.Join(tempDir, binaryName)
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/archguard-e2e")
	buildCmd.Dir = sourceRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, out)
	}

	fixturePath := filepath.Join(tempDir, fixtureFilename)

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
	indexCmd := exec.Command(binaryPath, "index")
	indexCmd.Dir = tempDir
	out, err := indexCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to index for E2E test: %v\nOutput: %s", err, string(out))
	}
	t.Logf("Index output: %s", string(out))

	t.Run("Detects violation in JS file", func(t *testing.T) {
		runCheck(t, tempDir, binaryPath, fixtureFilename, true)
	})

	t.Run("Passes after fixture removal", func(t *testing.T) {
		if err := os.Remove(fixturePath); err != nil {
			t.Fatalf("Failed to remove fixture: %v", err)
		}
		runCheck(t, tempDir, binaryPath, fixtureFilename, false)
	})
}

// runCheck executes the archguard check command.
func runCheck(t *testing.T, dir, binaryPath, target string, expectFail bool) {
	t.Helper()

	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		args := []string{"check"}
		if target != "" {
			args = append(args, target)
		}

		cmd := exec.Command(binaryPath, args...)
		cmd.Dir = dir
		cmd.Env = os.Environ()

		output, err := cmd.CombinedOutput()
		outputStr := string(output)
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				// This is a system error (e.g., binary not found), not a controlled failure
				lastErr = fmt.Errorf("system error executing command: %v", err)
				continue
			}
		}

		if expectFail {
			if exitCode != 0 {
				return
			}
			lastErr = fmt.Errorf("expected violation failure, but got success or unrelated error. Output: %s", outputStr)
		} else {
			if exitCode == 0 {
				return
			}
			lastErr = fmt.Errorf("expected success, but got error: %v. Output: %s", err, outputStr)
		}

		if i < maxRetries-1 {
			t.Logf("Retry %d/%d", i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
	}

	t.Fatalf("runCheck failed after %d retries: %v", maxRetries, lastErr)
}
