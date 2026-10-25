package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestStageExitCodeValues(t *testing.T) {
	if ExitStageUnavailable != 6 || ExitStagePrecondition != 7 {
		t.Fatalf("stage exit codes = %d and %d, want 6 and 7", ExitStageUnavailable, ExitStagePrecondition)
	}
}

func TestStageExitCodesAreDistinctFromExistingCodes(t *testing.T) {
	seen := map[ExitCode]string{}
	for name, code := range map[string]ExitCode{
		"success": ExitSuccess, "error": ExitError, "usage": ExitUsage, "config": ExitConfig,
		"drift": ExitDriftDetected, "index": ExitIndexError,
		"unavailable": ExitStageUnavailable, "precondition": ExitStagePrecondition,
		"interrupted": ExitInterrupted,
	} {
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share exit code %d", name, other, code)
		}

		seen[code] = name
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	return captureStream(t, &os.Stdout, fn)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	return captureStream(t, &os.Stderr, fn)
}

func captureStream(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	orig := *stream
	*stream = writer
	defer func() { *stream = orig }()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("failed to read pipe: %v", err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("failed to close pipe reader: %v", err)
	}

	return buf.String()
}

func setupExecuteTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	gitInit := exec.CommandContext(t.Context(), "git", "init")

	gitInit.Dir = repoRoot
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

	resolvedRoot, err := exec.CommandContext(t.Context(), "git", "-C", repoRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	cleanRoot := filepath.Clean(strings.TrimSpace(string(resolvedRoot)))

	t.Chdir(cleanRoot)

	return cleanRoot
}

func runCheckCapturingStderr(t *testing.T) string {
	t.Helper()

	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"archguard", "check"}

	var stderr string
	captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			if code, err := Execute(t.Context(), "test", ProviderFactories{}); code != ExitConfig || err == nil {
				t.Errorf("Execute() = (%d, %v), want exit %d: this repo has no archguard.yaml", code, err, ExitConfig)
			}
		})
	})

	return stderr
}

func TestExecute_MissingDotEnv_NoStderrWarning(t *testing.T) {
	setupExecuteTestRepo(t)

	stderr := runCheckCapturingStderr(t)

	if strings.Contains(stderr, ".env") {
		t.Errorf("expected no .env warning when .env is simply absent, got: %q", stderr)
	}
}

func TestExecute_MalformedDotEnv_PrintsStderrWarning(t *testing.T) {
	cleanRoot := setupExecuteTestRepo(t)

	if err := os.WriteFile(filepath.Join(cleanRoot, ".env"), []byte(`KEY="unterminated`), 0644); err != nil {
		t.Fatalf("failed to write malformed .env: %v", err)
	}

	stderr := runCheckCapturingStderr(t)

	if !strings.Contains(stderr, "failed to load .env") {
		t.Errorf("expected a .env parse-failure warning on stderr, got: %q", stderr)
	}
}

func TestExecute_TopLevelHelpExitsSuccess(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, help := range []string{"--help", "-h", "help"} {
		t.Run(help, func(t *testing.T) {
			os.Args = []string{"archguard", help}
			var exitCode ExitCode
			var err error

			captured := captureStdout(t, func() {
				exitCode, err = Execute(t.Context(), "test", ProviderFactories{})
			})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if exitCode != ExitSuccess {
				t.Fatalf("expected exit code %d, got %d", ExitSuccess, exitCode)
			}

			if !strings.Contains(captured, "Usage: archguard") {
				t.Fatalf("expected usage output, got %q", captured)
			}
		})
	}
}

func TestEnterRepoRoot_SymlinkedWorkingDirectory(t *testing.T) {
	repoRoot := setupExecuteTestRepo(t)
	link := filepath.Join(t.TempDir(), "link")

	if err := os.Symlink(repoRoot, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating a symlink on Windows needs Developer Mode or admin: %v", err)
		}

		t.Fatalf("creating symlink: %v", err)
	}

	t.Chdir(link)

	inv := &invocation{command: "check", check: checkCmd{Paths: []string{"a.go", filepath.Join(link, "b.go")}}}

	if _, err := enterRepoRoot(t.Context(), inv); err != nil {
		t.Fatalf("enterRepoRoot() error = %v", err)
	}

	if want := []string{"a.go", "b.go"}; !slices.Equal(inv.check.Paths, want) {
		t.Errorf("Paths = %v, want %v", inv.check.Paths, want)
	}
}
