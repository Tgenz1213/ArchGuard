package analysis_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

const fakeGitStartedEnv = "ARCHGUARD_FAKE_GIT_STARTED"

// Re-executed as a fake git binary by TestRun_CancelKillsInFlightGit.
func TestMain(m *testing.M) {
	if marker := os.Getenv(fakeGitStartedEnv); marker != "" {
		_ = os.WriteFile(marker, nil, 0o600)

		time.Sleep(30 * time.Second)
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestRun_CancelKillsInFlightGit(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()

	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}

	copyFile(t, self, filepath.Join(binDir, name))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv(fakeGitStartedEnv, marker)

	engine := analysis.NewEngine(&config.Config{}, index.NewLocalStore(5), &llm.MockProvider{}, &analysis.StagedProvider{}, false, false)
	engine.Cache = nil
	engine.Writer = io.Discard

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- engine.Run(ctx) }()

	// Cancelling before git starts would pass without ever exercising the kill.
	waitForFile(t, marker, 10*time.Second)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run still blocked 10s after cancel: the git subprocess was not killed")
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("fake git never started (no %s after %s)", path, timeout)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatal(err)
	}
}
