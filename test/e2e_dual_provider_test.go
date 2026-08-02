package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const (
	chatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	embedProviderMarker = "Using Mock Embed LLM Provider (E2E)"
)

// Drives index+check against differing llm.provider/vector_store.provider names, asserting via stdout markers that index uses the embed mock and check uses both.
func TestE2E_DualProvider_IndexAndCheckUseDistinctProviders(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "claude"
vector_store:
  provider: "openai"
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

	t.Run("Index uses the embed provider", func(t *testing.T) {
		output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))

		if !strings.Contains(output, embedProviderMarker) {
			t.Fatalf("expected index output to contain %q, got: %s", embedProviderMarker, output)
		}
	})

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	fixtureContent := fmt.Sprintf(`
function sensitiveData() {
    console.log("%s: 123");
}
`, testutil.MockViolationTrigger)

	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	t.Run("Check with a violation uses both providers", func(t *testing.T) {
		output := runCheckCapture(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))

		if !strings.Contains(output, chatProviderMarker) {
			t.Fatalf("expected check output to contain %q, got: %s", chatProviderMarker, output)
		}
		if !strings.Contains(output, embedProviderMarker) {
			t.Fatalf("expected check output to contain %q, got: %s", embedProviderMarker, output)
		}
	})

	t.Run("Check without a violation still succeeds end-to-end", func(t *testing.T) {
		if err := os.Remove(fixturePath); err != nil {
			t.Fatalf("Failed to remove fixture: %v", err)
		}
		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitSuccess))
	})
}

// Proves llm.provider: claude with vector_store.provider unset exits ExitConfig before any provider is constructed.
func TestE2E_DualProvider_ConfigValidationFailsFast(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "claude"
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

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitConfig))

	if strings.Contains(output, chatProviderMarker) || strings.Contains(output, embedProviderMarker) {
		t.Fatalf("expected no provider to be constructed before config validation fails, got output: %s", output)
	}
}

// Like runIndexCmd but returns captured output for marker assertions.
func runIndexCmdCapture(t *testing.T, dir, binaryPath string, expectedExitCode int) string {
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

	return outputStr
}

// Like runCheck but returns captured output; no retry since the mocks are deterministic.
func runCheckCapture(t *testing.T, dir, binaryPath, target string, expectedExitCode int) string {
	t.Helper()

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

	if exitCode != expectedExitCode {
		t.Fatalf("expected check exit code %d, but got %d. Output: %s", expectedExitCode, exitCode, outputStr)
	}

	return outputStr
}
