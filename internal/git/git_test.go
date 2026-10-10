package git

import (
	"context"
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

func TestGetChangedSince(t *testing.T) {
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

	got, err := changedPaths(t.Context(), "HEAD~1")
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

	got, err := changedPaths(t.Context(), "HEAD")
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

	got, err := changedPaths(t.Context(), "same.go")
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
			_, err := changedPaths(t.Context(), ref)
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

func changedPaths(ctx context.Context, ref string) ([]string, error) {
	files, err := GetChangedSince(ctx, ref)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}

	return paths, nil
}

func renameFixture(t *testing.T, edit string) string {
	t.Helper()

	dir := initTestRepo(t)
	body := "package a\n\nfunc keep() {\n\tlog(\"password: old\")\n}\n"

	writeFile(t, dir, "old.go", body)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "mv", "old.go", "new.go")
	writeFile(t, dir, "new.go", body+edit)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "rename")

	return dir
}

func TestGetChangedSince_ReportsTheOldPathOfARename(t *testing.T) {
	renameFixture(t, "")

	got, err := GetChangedSince(t.Context(), "HEAD~1")
	want := []ChangedFile{{Path: "new.go", OldPath: "old.go"}}

	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("GetChangedSince() = %+v, %v; want %+v", got, err, want)
	}
}

func TestGetDiffSince_ARenamedFileShowsOnlyItsRealChanges(t *testing.T) {
	tests := []struct {
		name     string
		edit     string
		wantEdit bool
	}{
		{"a pure rename has no changed lines", "", false},
		{"a rename with an edit shows only the edit", "\n// edited\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renameFixture(t, tt.edit)

			got, err := GetDiffSince(t.Context(), "HEAD~1", "old.go", "new.go")
			if err != nil {
				t.Fatalf("GetDiffSince: %v", err)
			}

			if strings.Contains(got, "+package a") {
				t.Fatalf("untouched lines are reported as added:\n%s", got)
			}

			if strings.Contains(got, "+// edited") != tt.wantEdit {
				t.Fatalf("edit present = %v, want %v:\n%s", !tt.wantEdit, tt.wantEdit, got)
			}
		})
	}
}
