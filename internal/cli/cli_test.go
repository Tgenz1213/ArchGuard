package cli

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
)

func TestExitCodeForAnalysisError(t *testing.T) {
	t.Run("returns drift exit code for direct drift detection errors", func(t *testing.T) {
		err := &analysis.DriftDetectedError{Count: 2}
		if got := exitCodeForAnalysisError(err); got != ExitDriftDetected {
			t.Fatalf("expected %d, got %d", ExitDriftDetected, got)
		}
	})

	t.Run("returns drift exit code for wrapped drift detection errors", func(t *testing.T) {
		err := fmt.Errorf("wrapped: %w", &analysis.DriftDetectedError{Count: 2})
		if got := exitCodeForAnalysisError(err); got != ExitDriftDetected {
			t.Fatalf("expected %d, got %d", ExitDriftDetected, got)
		}
	})

	t.Run("returns generic error exit code for operational errors", func(t *testing.T) {
		err := errors.New("git content provider failure")
		if got := exitCodeForAnalysisError(err); got != ExitError {
			t.Fatalf("expected %d, got %d", ExitError, got)
		}
	})
}

func TestLoadDotEnv(t *testing.T) {
	t.Run("sets unset variables from .env file", func(t *testing.T) {
		t.Chdir(t.TempDir())

		envContent := "FOO_TEST_VAR=hello\nBAR_TEST_VAR=\"quoted value\"\n"
		if err := os.WriteFile(".env", []byte(envContent), 0644); err != nil {
			t.Fatalf("failed to write .env: %v", err)
		}

		os.Unsetenv("FOO_TEST_VAR")
		os.Unsetenv("BAR_TEST_VAR")
		defer os.Unsetenv("FOO_TEST_VAR")
		defer os.Unsetenv("BAR_TEST_VAR")

		if err := loadDotEnv(); err != nil {
			t.Fatalf("loadDotEnv failed: %v", err)
		}

		if got := os.Getenv("FOO_TEST_VAR"); got != "hello" {
			t.Errorf("expected FOO_TEST_VAR=hello, got %q", got)
		}
		if got := os.Getenv("BAR_TEST_VAR"); got != "quoted value" {
			t.Errorf("expected BAR_TEST_VAR='quoted value', got %q", got)
		}
	})

	t.Run("does not override an already-set variable", func(t *testing.T) {
		t.Chdir(t.TempDir())

		if err := os.WriteFile(".env", []byte("EXISTING_TEST_VAR=fromfile\n"), 0644); err != nil {
			t.Fatalf("failed to write .env: %v", err)
		}

		t.Setenv("EXISTING_TEST_VAR", "fromenv")

		if err := loadDotEnv(); err != nil {
			t.Fatalf("loadDotEnv failed: %v", err)
		}

		if got := os.Getenv("EXISTING_TEST_VAR"); got != "fromenv" {
			t.Errorf("expected existing value to be preserved, got %q", got)
		}
	})

	t.Run("returns an error when .env is missing", func(t *testing.T) {
		t.Chdir(t.TempDir())

		if err := loadDotEnv(); err == nil {
			t.Fatal("expected error when .env does not exist, got nil")
		}
	})
}
