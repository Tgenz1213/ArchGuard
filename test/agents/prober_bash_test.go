package agents

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

const proberHook = "../../.claude/hooks/prober-bash.sh"

func findBash(t *testing.T) string {
	t.Helper()

	if runtime.GOOS != "windows" {
		path, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash not found")
		}

		return path
	}

	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found, so Git Bash cannot be located")
	}

	for _, rel := range []string{"../bin/bash.exe", "../../bin/bash.exe"} {
		candidate := filepath.Join(filepath.Dir(git), filepath.FromSlash(rel))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	t.Skip("Git Bash not found next to git")

	return ""
}

func runHook(t *testing.T, input string) int {
	t.Helper()

	script, err := filepath.Abs(proberHook)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), findBash(t), filepath.ToSlash(script))
	cmd.Stdin = bytes.NewBufferString(input)

	err = cmd.Run()
	if err == nil {
		return 0
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}

	t.Fatal(err)

	return -1
}

func hookInput(t *testing.T, command string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]map[string]string{"tool_input": {"command": command}})
	if err != nil {
		t.Fatal(err)
	}

	return string(payload)
}

func TestProberBashHook(t *testing.T) {
	allowed := []string{
		"go test ./internal/analysis -run TestX -v",
		"go test -race ./...",
		"go build -o archguard ./cmd/archguard",
		"go vet ./...",
		"go run ./cmd/archguard check --all",
		"golangci-lint run --timeout=5m",
		"git diff main...HEAD",
		"git apply /tmp/review.patch",
		"git restore internal/x.go",
		"git reset --hard HEAD",
		"git clean -fd",
		"git checkout --detach refs/review/pr-12",
		"git checkout --detach 0123abcd",
		"gh pr view 12",
		"gh pr diff 12",
		"gh issue view 485",
		"git log --oneline -5",
	}
	refused := []string{
		"git commit -m x",
		"git push origin x",
		"git stash",
		"git reset --hard origin/main",
		"git clean -fdx",
		"git checkout main",
		"git branch -D x",
		"git switch main",
		"git config user.name x",
		"git worktree add ../x",
		"git fetch origin pull/12/head",
		"go test -exec 'git push' ./...",
		"go build -toolexec x ./...",
		"go test -overlay x.json ./...",
		"go test -overlay=x.json ./...",
		"git -C ../other status",
		"cd ..",
		"go test ./... && git push",
		"go test ./... ; rm x",
		"go test $(whoami)",
		"gh pr comment 12",
		"gh api repos/x/y",
		"",
		"git log --output=x",
		"git diff --op=x",
		"git grep -O less x",
		"git diff -Ox",
		"git checkout --detach main",
		"git checkout --detach refs/review/pr-12x",
		"git checkout --detach 012345",
		"git reset --hard HEAD~1",
		"git clean -fd .",
		"go test -C ../other ./...",
		"git diff -C x",
		"go build -o /tmp/x ./...",
		"go build -o=/tmp/x ./...",
		"go build -o ../x ./cmd/archguard",
		"go build -o C:/x ./cmd/archguard",
		"go test -coverprofile=/tmp/c.out ./...",
		"go test --coverprofile=/tmp/c.out ./...",
		"go test -test.cpuprofile=/tmp/c.out ./...",
		"go build -o ~/x ./cmd/archguard",
		"go test -cpuprofile cpu.out ./...",
		"go test -memprofile=m.out ./...",
		"go test -blockprofile b.out ./...",
		"go test -mutexprofile m.out ./...",
		"go test -trace t.out ./...",
		"go test -outputdir ../x ./...",
		"git apply --unsafe-paths x.patch",
		"git apply --directory=../x x.patch",
		"golangci-lint run --fix",
	}

	for _, command := range allowed {
		t.Run("allow "+command, func(t *testing.T) {
			if got := runHook(t, hookInput(t, command)); got != 0 {
				t.Errorf("exit = %d, want 0", got)
			}
		})
	}

	for _, command := range refused {
		t.Run("refuse "+command, func(t *testing.T) {
			if got := runHook(t, hookInput(t, command)); got != 2 {
				t.Errorf("exit = %d, want 2", got)
			}
		})
	}

	t.Run("refuse unparseable input", func(t *testing.T) {
		if got := runHook(t, "not json"); got != 2 {
			t.Errorf("exit = %d, want 2", got)
		}
	})
}
