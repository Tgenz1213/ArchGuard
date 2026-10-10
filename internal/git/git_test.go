package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// quotepath=true is git's real default; the test must run against it, not an environment where it is off.
func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "core.quotepath", "true")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	t.Chdir(dir)

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)

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

	got, err := GetAllTrackedFiles(t.Context())
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

	got, err := GetStagedFiles(t.Context())
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

	got, err := GetUncommittedFiles(t.Context())
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

func TestGetFilesChangedSince(t *testing.T) {
	dir := initTestRepo(t)

	writeFile(t, dir, "keep.go", "package a\n")
	writeFile(t, dir, "edit.go", "package a\n")
	writeFile(t, dir, "gone.go", "package a\n")
	writeFile(t, dir, "old.go", "package a\n// rename me\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")

	writeFile(t, dir, "edit.go", "package a\n// changed\n")
	writeFile(t, dir, "new.go", "package a\n")
	writeFile(t, dir, "café.go", "package a\n")
	runGit(t, dir, "rm", "-q", "gone.go")
	runGit(t, dir, "mv", "old.go", "renamed.go")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "change")

	got, err := GetFilesChangedSince(t.Context(), "HEAD~1")
	if err != nil {
		t.Fatalf("GetFilesChangedSince: %v", err)
	}

	sort.Strings(got)

	want := []string{"café.go", "edit.go", "new.go", "renamed.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v (deleted files excluded, renames under the new name)", got, want)
	}
}

func TestGetFilesChangedSince_NothingChangedIsEmpty(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, dir, "a.go", "package a\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")

	got, err := GetFilesChangedSince(t.Context(), "HEAD")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no files and no error", got, err)
	}
}

func TestGetFilesChangedSince_RefNamedLikeAFile(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, dir, "same.go", "package a\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "branch", "same.go")
	writeFile(t, dir, "other.go", "package a\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "change")

	got, err := GetFilesChangedSince(t.Context(), "same.go")
	if err != nil || !slices.Equal(got, []string{"other.go"}) {
		t.Fatalf("got %v, %v; want [other.go] for a branch that shares its name with a file", got, err)
	}
}

func TestGetFilesChangedSince_RejectsBadRefs(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, dir, "a.go", "package a\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")

	for _, ref := range []string{"does-not-exist", "--output=/tmp/x", "-1"} {
		t.Run(ref, func(t *testing.T) {
			_, err := GetFilesChangedSince(t.Context(), ref)
			if err == nil || !strings.Contains(err.Error(), ref) {
				t.Fatalf("expected an error naming %q, got %v", ref, err)
			}
		})
	}
}

func TestGetDiffSince_ReturnsOnlyThatFilesChange(t *testing.T) {
	dir := initTestRepo(t)
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "b.go", "package b\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	writeFile(t, dir, "a.go", "package a\n// added line\n")
	writeFile(t, dir, "b.go", "package b\n// other\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "change")

	got, err := GetDiffSince(t.Context(), "HEAD~1", "a.go")
	if err != nil || !strings.Contains(got, "+// added line") || strings.Contains(got, "-// added line") || strings.Contains(got, "other") {
		t.Fatalf("diff = %q, err = %v; want only a.go's change, as additions", got, err)
	}
}
