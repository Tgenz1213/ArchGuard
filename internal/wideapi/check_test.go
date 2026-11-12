package wideapi

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const importers = 5

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return string(out)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// newRepo is a module whose core package is imported by five others and whose lone package by one, committed on main.
func newRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "config", "commit.gpgsign", "false")

	write(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
	write(t, dir, "core/core.go", "package core\n\nfunc F(a int) int { return a }\n")
	write(t, dir, "core/extra.go", "package core\n\nfunc Extra() {}\n")
	write(t, dir, "core/core_test.go", "package core\n")
	write(t, dir, "lone/lone.go", "package lone\n\nfunc F() {}\n")

	for i := range importers {
		write(t, dir, fmt.Sprintf("p%d/p.go", i), fmt.Sprintf("package p%d\n\nimport \"example.com/m/core\"\n\nvar _ = core.F\n", i))
	}

	write(t, dir, "p0/lone.go", "package p0\n\nimport \"example.com/m/lone\"\n\nvar _ = lone.F\n")

	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")

	return dir
}

// onBranch commits the edit on a branch and leaves main checked out, so the branch is a commit that is not the working tree.
func onBranch(t *testing.T, dir string, edit func(t *testing.T, dir string)) {
	t.Helper()

	git(t, dir, "checkout", "-q", "-b", "feature")
	edit(t, dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "change")
	git(t, dir, "checkout", "-q", "main")
}

func changes(r Result) map[string][]string {
	got := map[string][]string{}
	for _, c := range r.Changes {
		got[c.Dir] = c.Names
	}

	return got
}

func TestCheckReportsAChangeToAWidePackage(t *testing.T) {
	tests := []struct {
		name string
		edit func(t *testing.T, dir string)
		want map[string][]string
	}{
		{"exported func added", func(t *testing.T, dir string) {
			write(t, dir, "core/core.go", "package core\n\nfunc F(a int) int { return a }\n\nfunc G() {}\n")
		}, map[string][]string{"core": {"+func G"}}},
		{"parameter retyped", func(t *testing.T, dir string) {
			write(t, dir, "core/core.go", "package core\n\nfunc F(a string) int { return 0 }\n")
		}, map[string][]string{"core": {"~func F"}}},
		{"file deleted", func(t *testing.T, dir string) {
			require.NoError(t, os.Remove(filepath.Join(dir, "core", "extra.go")))
		}, map[string][]string{"core": {"-func Extra"}}},
		{"unexported func added", func(t *testing.T, dir string) {
			write(t, dir, "core/core.go", "package core\n\nfunc F(a int) int { return a }\n\nfunc g() {}\n")
		}, map[string][]string{}},
		{"test file edited", func(t *testing.T, dir string) {
			write(t, dir, "core/core_test.go", "package core\n\nfunc G() {}\n")
		}, map[string][]string{}},
		{"package below the reach", func(t *testing.T, dir string) {
			write(t, dir, "lone/lone.go", "package lone\n\nfunc F() {}\n\nfunc G() {}\n")
		}, map[string][]string{}},
		{"package that gains importers in the branch", func(t *testing.T, dir string) {
			write(t, dir, "lone/lone.go", "package lone\n\nfunc F() {}\n\nfunc G() {}\n")

			for i := 1; i < importers; i++ {
				write(t, dir, fmt.Sprintf("p%d/lone.go", i), fmt.Sprintf("package p%d\n\nimport \"example.com/m/lone\"\n\nvar _ = lone.F\n", i))
			}
		}, map[string][]string{"lone": {"+func G"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := newRepo(t)
			onBranch(t, dir, tt.edit)

			result, err := Check(t.Context(), dir, "main", "feature", DefaultMinReach)

			require.NoError(t, err)
			assert.Empty(t, result.Warnings)
			assert.Equal(t, tt.want, changes(result))
		})
	}
}

func TestCheckReadsTheCheckedOutCommitWithoutAWorktree(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "core/core.go", "package core\n\nfunc F(a int) int { return a }\n\nfunc G() {}\n")
	git(t, dir, "commit", "-q", "-am", "change")

	result, err := Check(t.Context(), dir, "main", "HEAD", DefaultMinReach)

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"core": {"+func G"}}, changes(result))
	assert.Equal(t, 1, bytes.Count([]byte(git(t, dir, "worktree", "list")), []byte("\n")))
}

func TestCheckDoesNotReportWhatMainChangedAfterTheBranchForked(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	onBranch(t, dir, func(t *testing.T, dir string) {
		write(t, dir, "p1/own.go", "package p1\n")
	})
	write(t, dir, "core/core.go", "package core\n\nfunc F(a int) int { return a }\n\nfunc OnMain() {}\n")
	git(t, dir, "commit", "-q", "-am", "main moves on")

	result, err := Check(t.Context(), dir, "main", "feature", DefaultMinReach)

	require.NoError(t, err)
	assert.Empty(t, result.Changes)
}

func TestCheckWarnsAboutAPackageItCannotParse(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	onBranch(t, dir, func(t *testing.T, dir string) {
		write(t, dir, "core/core.go", "package core\n\nfunc (")
	})

	result, err := Check(t.Context(), dir, "main", "feature", DefaultMinReach)

	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "core not checked")
}

func TestCheckFailsForARefThatDoesNotExist(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)

	_, err := Check(t.Context(), dir, "main", "nope", DefaultMinReach)

	require.Error(t, err)
}

func TestStringNamesEachChangedPackageAndWarning(t *testing.T) {
	result := Result{
		Changes:  []Change{{Dir: "core", Reach: 6, Names: []string{"+func A", "+func B", "+func C", "+func D", "+func E", "+func F", "+func G", "+func H"}}},
		Warnings: []string{"index not checked: boom"},
	}

	out := result.String(5)

	assert.Contains(t, out, "wideapi: index not checked: boom")
	assert.Contains(t, out, "5+ packages import")
	assert.Contains(t, out, "core (reach 6): +func A, +func B, +func C, +func D, +func E, +func F, and 2 more")
}

func TestStringSaysNothingWhenNothingChanged(t *testing.T) {
	assert.Empty(t, Result{}.String(5))
}
