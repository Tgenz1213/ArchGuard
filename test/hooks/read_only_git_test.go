package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	hooksDir    = "../../.claude/hooks"
	readOnlyGit = "read-only-git.sh"
	blockEnv    = "block-env.sh"
)

func TestReadOnlyGitHook(t *testing.T) {
	bash := findBash(t)

	tests := []struct {
		name     string
		command  string
		wantCode int
	}{
		{"git diff", "git diff", 0},
		{"git diff with args", "git diff --stat origin/main...HEAD -- internal/", 0},
		{"git log", "git log --oneline -5", 0},
		{"git show", "git show HEAD:go.mod", 0},
		{"git status", "git status --short", 0},
		{"git grep", "git grep -n 'func main' -- '*.go'", 0},
		{"git ls-files", "git ls-files internal/", 0},
		{"gh issue view", "gh issue view 396 --comments", 0},
		{"gh issue list", "gh issue list --state open --limit 100", 0},
		{"gh pr view", "gh pr view 415 --json files", 0},
		{"gh pr list", "gh pr list --search 'is:merged 412'", 0},
		{"gh pr diff", "gh pr diff 415", 0},
		{"gh issue edit", "gh issue edit 396 --add-label bug", 2},
		{"gh issue comment", "gh issue comment 396 --body x", 2},
		{"gh pr merge", "gh pr merge 415", 2},
		{"gh api", "gh api repos/o/r/issues/1 -X PATCH", 2},
		{"gh view then edit", "gh issue view 1 && gh issue close 1", 2},
		{"git stash", "git stash", 2},
		{"git reset --hard", "git reset --hard", 2},
		{"git branch", "git branch x", 2},
		{"touch", "touch x", 2},
		{"diff --output", "git diff --output=x", 2},
		{"log --output", "git log -p --output x", 2},
		{"grep -O pager", "git grep -Otouch main", 2},
		{"grep bundled -O", "git grep -nOtouch main", 2},
		{"grep --open-files-in-pager", "git grep --open-files-in-pager=touch main", 2},
		{"grep abbreviated --open-files-in-pager", "git grep --op=touch main", 2},
		{"cd then diff", "cd internal && git diff", 2},
		{"diff then checkout", "git diff --stat && git checkout .", 2},
		{"semicolon", "git status; rm go.mod", 2},
		{"pipe", "git log | tee x", 2},
		{"redirect", "git show HEAD > x", 2},
		{"substitution", "git show $(touch x)", 2},
		{"backtick", "git show `touch x`", 2},
		{"newline", "git status\nrm go.mod", 2},
		{"global option", "git -C .. status", 2},
		{"empty", "", 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runHook(t, bash, readOnlyGit, tc.command); got != tc.wantCode {
				t.Errorf("exit code for %q = %d, want %d", tc.command, got, tc.wantCode)
			}
		})
	}
}

func TestReadOnlyGitHookIgnoresTheDescription(t *testing.T) {
	bash := findBash(t)

	input := hookInput(t, map[string]string{
		"command":     "git stash",
		"description": `"command": "git status"`,
	})

	if got := runHookWithInput(t, bash, readOnlyGit, input); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
}

func runHook(t *testing.T, bash, script, command string) int {
	t.Helper()

	return runHookWithInput(t, bash, script, hookInput(t, map[string]string{
		"command":     command,
		"description": "Probe the hook",
	}))
}

// hookInput encodes the PreToolUse payload the way Claude Code does, which
// leaves <, > and & unescaped.
func hookInput(t *testing.T, toolInput map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	err := enc.Encode(map[string]any{
		"session_id":      "test",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      toolInput,
	})
	if err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func runHookWithInput(t *testing.T, bash, script string, input []byte) int {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), bash, script)
	cmd.Dir = hooksDir
	cmd.Stdin = bytes.NewReader(input)

	err := cmd.Run()
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run hook: %v", err)
	}

	return exitErr.ExitCode()
}

// findBash prefers Git Bash on Windows, where bash on PATH can be the WSL
// launcher instead.
func findBash(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		if git, err := exec.LookPath("git"); err == nil {
			gitBash := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe")
			if _, err := os.Stat(gitBash); err == nil {
				return gitBash
			}
		}
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not found: %v", err)
	}

	return bash
}
