package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// quotepath=true is git's real default; the fix must work against it, not an environment where it's off.
func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "core.quotepath", "true")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir into fixture repo: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	})

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\noutput: %s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}

var nonASCIINames = []string{
	"café.go",
	"日本語.go",
}

func TestGetAllTrackedFiles_NonASCIINames(t *testing.T) {
	dir := initTestRepo(t)

	for _, name := range nonASCIINames {
		writeFile(t, dir, name, "package main\n")
		runGit(t, dir, "add", "--", name)
	}
	runGit(t, dir, "commit", "-m", "add non-ascii files")

	got, err := GetAllTrackedFiles()
	if err != nil {
		t.Fatalf("GetAllTrackedFiles failed: %v", err)
	}
	assertContainsExactly(t, got, nonASCIINames)
}

func TestGetStagedFiles_NonASCIINames(t *testing.T) {
	dir := initTestRepo(t)

	for _, name := range nonASCIINames {
		writeFile(t, dir, name, "package main\n")
		runGit(t, dir, "add", "--", name)
	}

	got, err := GetStagedFiles()
	if err != nil {
		t.Fatalf("GetStagedFiles failed: %v", err)
	}
	assertContainsExactly(t, got, nonASCIINames)
}

func TestGetUncommittedFiles_NonASCIINames(t *testing.T) {
	dir := initTestRepo(t)

	for _, name := range nonASCIINames {
		writeFile(t, dir, name, "package main\n")
		runGit(t, dir, "add", "--", name)
	}
	runGit(t, dir, "commit", "-m", "add non-ascii files")

	for _, name := range nonASCIINames {
		writeFile(t, dir, name, "package main\n\nfunc main() {}\n")
	}

	got, err := GetUncommittedFiles()
	if err != nil {
		t.Fatalf("GetUncommittedFiles failed: %v", err)
	}
	assertContainsExactly(t, got, nonASCIINames)
}

func assertContainsExactly(t *testing.T, got, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)

	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("expected %v, got %v", wantSorted, gotSorted)
	}
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Fatalf("expected %v, got %v (path not exact match -- likely still C-quoted/escaped)", wantSorted, gotSorted)
		}
	}
}
