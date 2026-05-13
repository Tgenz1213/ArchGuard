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

	rootDir := filepath.Dir(wd)
	if filepath.Base(wd) != "test" {
		rootDir = wd
	}

	t.Log("Building archguard binary for E2E test...")
	buildCmd := exec.Command("go", "build", "-o", binaryName, "./cmd/archguard-e2e")
	buildCmd.Dir = rootDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, out)
	}

	binaryPath := filepath.Join(rootDir, binaryName)
	t.Cleanup(func() { os.Remove(binaryPath) })

	fixturePath := filepath.Join(rootDir, fixtureFilename)
	t.Cleanup(func() { os.Remove(fixturePath) })
	if err := os.Remove(fixturePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Initial cleanup failed: %v", err)
	}

	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	adrPath := filepath.Join(rootDir, "docs", "arch", "0000-no-secrets-in-log.md")
	adrContent := `---
title: "No Secrets in Logs"
status: "Accepted"
scope: "**"
---

## Context
Logging sensitive data is a security risk.

## Decision
Do not print passwords or secrets to console.log.`

	t.Cleanup(func() { os.Remove(adrPath) })
	if err := os.MkdirAll(filepath.Dir(adrPath), 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	if err := os.WriteFile(adrPath, []byte(adrContent), 0644); err != nil {
		t.Fatalf("Failed to create mock ADR: %v", err)
	}

	t.Log("Indexing ADRs for E2E test...")
	indexCmd := exec.Command(binaryPath, "index")
	indexCmd.Dir = rootDir
	if out, err := indexCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to index for E2E test: %v\nOutput: %s", err, out)
	}

	t.Run("Detects violation in JS file", func(t *testing.T) {
		runCheck(t, rootDir, binaryPath, fixtureFilename, true)
	})

	t.Run("Passes after fixture removal", func(t *testing.T) {
		if err := os.Remove(fixturePath); err != nil {
			t.Fatalf("Failed to remove fixture: %v", err)
		}
		runCheck(t, rootDir, binaryPath, fixtureFilename, false)
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
