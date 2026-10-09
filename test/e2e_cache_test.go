//go:build e2e

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

func TestE2E_CheckWritesAnalysisCache(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)
	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))
	runCheck(t, dir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))

	entries, err := filepath.Glob(filepath.Join(dir, ".archguard", "cache", "*.json"))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) == 0 {
		t.Fatal("a check that called the LLM wrote no analysis cache entry")
	}
}

func TestE2E_UnusableCacheDirWarnsAndRunsUncached(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)
	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	cachePath := filepath.Join(dir, ".archguard", "cache")
	if err := os.RemoveAll(cachePath); err != nil {
		t.Fatal(err)
	}

	// A regular file where the cache directory belongs makes creating it fail.
	if err := os.WriteFile(cachePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), binaryPath, "check", "clean.js")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check should still succeed without a cache: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "Warning: analysis cache disabled") {
		t.Errorf("expected a cache-disabled warning, got:\n%s", out)
	}
}
