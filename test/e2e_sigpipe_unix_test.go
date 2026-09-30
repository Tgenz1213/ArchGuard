//go:build unix

package test

import (
	"os"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

// brokenPipe returns the write end of a pipe whose reader is already closed.
func brokenPipe(t *testing.T) *os.File {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("closing pipe writer: %v", err)
		}
	})

	return w
}

func TestE2E_BrokenStdoutPipeExitsOneInsteadOfSIGPIPE(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)

	for _, args := range [][]string{{"--help"}, {"index"}} {
		t.Run(args[0], func(t *testing.T) {
			// A process killed by SIGPIPE reports exit code -1 here, not 1.
			if code := runWithStreams(t, dir, binaryPath, brokenPipe(t), os.Stderr, args...); code != int(cli.ExitError) {
				t.Fatalf("exit code = %d, want %d", code, cli.ExitError)
			}
		})
	}
}

func TestE2E_BrokenStderrPipeKeepsExitCode(t *testing.T) {
	dir, binaryPath := setupOutputErrorsRepo(t)
	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	stdout, err := os.Create(t.TempDir() + "/stdout.json")
	if err != nil {
		t.Fatal(err)
	}

	code := runWithStreams(t, dir, binaryPath, stdout, brokenPipe(t), "check", "--format", "json", "--debug", fixtureFilename)

	if err := stdout.Close(); err != nil {
		t.Fatal(err)
	}

	if code != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code = %d, want %d: a broken stderr pipe must not change the verdict", code, cli.ExitDriftDetected)
	}
}
