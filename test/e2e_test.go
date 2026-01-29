package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	defer os.Remove(binaryPath)

	fixturePath := filepath.Join(rootDir, fixtureFilename)
	os.Remove(fixturePath)
	defer os.Remove(fixturePath)

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

	if err := os.MkdirAll(filepath.Dir(adrPath), 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	if err := os.WriteFile(adrPath, []byte(adrContent), 0644); err != nil {
		t.Fatalf("Failed to create mock ADR: %v", err)
	}
	defer os.Remove(adrPath)

	t.Log("Indexing ADRs for E2E test...")
	indexCmd := exec.Command(binaryPath, "index")
	indexCmd.Dir = rootDir
	if out, err := indexCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to index for E2E test: %v\nOutput: %s", err, out)
	}

	t.Log("Running scan with violation file...")
	if err := runCheck(t, rootDir, binaryPath, fixtureFilename, true); err != nil {
		t.Fatalf("Scan failed expectation (expected to catch violation): %v", err)
	}

	if err := os.Remove(fixturePath); err != nil {
		t.Fatalf("Failed to remove fixture: %v", err)
	}

	t.Log("Running scan without violation file...")
	if err := runCheck(t, rootDir, binaryPath, fixtureFilename, false); err != nil {
		t.Fatalf("Scan failed expectation (expected to pass): %v", err)
	}
}

// runCheck executes the archguard check command with retries to account for environment flakiness.
func runCheck(t *testing.T, dir, binaryPath, target string, expectFail bool) error {
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
		cmdFailed := err != nil

		if expectFail {
			if cmdFailed && (strings.Contains(outputStr, "found") || strings.Contains(outputStr, "violation")) {
				return nil
			}
			lastErr = fmt.Errorf("expected violation failure, but got success or unrelated error. Output: %s", outputStr)
		} else {
			if !cmdFailed {
				return nil
			}
			lastErr = fmt.Errorf("expected success, but got error: %v. Output: %s", err, outputStr)
		}

		if i < maxRetries-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return lastErr
}
