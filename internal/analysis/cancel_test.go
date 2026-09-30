package analysis_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/output"
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
	engine.Out = output.Discard()

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

type cancelOnReadProvider struct {
	files  []string
	cancel context.CancelFunc
	reads  atomic.Int32
}

func (p *cancelOnReadProvider) GetFiles(context.Context) ([]string, error) { return p.files, nil }

func (p *cancelOnReadProvider) GetContent(context.Context, string) (string, error) {
	p.reads.Add(1)
	p.cancel()

	return "content", nil
}

func (p *cancelOnReadProvider) GetDiff(context.Context, string) (string, error) { return "", nil }

func TestRun_CancelStopsSchedulingAndSkipsBaseline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	content := &cancelOnReadProvider{files: []string{"a.go", "b.go", "c.go"}, cancel: cancel}

	cfg := &config.Config{Analysis: config.Analysis{MaxConcurrency: 1}}

	engine := analysis.NewEngine(cfg, index.NewLocalStore(5), &llm.MockProvider{}, content, false, false)
	engine.Cache = nil
	engine.Out = output.Discard()
	engine.UpdateBaseline = true

	err := engine.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}

	if got := content.reads.Load(); got != 1 {
		t.Errorf("files read after cancel: got %d reads, want 1 (no new files scheduled once cancelled)", got)
	}

	if engine.CollectedBaseline != nil {
		t.Errorf("CollectedBaseline = %+v, want nil so a partial scan is never saved", engine.CollectedBaseline)
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
