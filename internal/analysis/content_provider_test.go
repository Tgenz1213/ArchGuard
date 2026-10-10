package analysis

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func commitFiles(t *testing.T, dir, message string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for _, args := range [][]string{{"add", "."}, {"commit", "-m", message}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)

		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)

		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	return dir
}

func TestChangedSinceProvider_DiffOfARenamedFileShowsOnlyItsRealChanges(t *testing.T) {
	dir := initRepo(t)
	body := "package a\n\nfunc keep() {\n\tlog(\"password: old\")\n}\n"

	commitFiles(t, dir, "base", map[string]string{"old.go": body})

	if err := os.Remove(filepath.Join(dir, "old.go")); err != nil {
		t.Fatalf("remove old.go: %v", err)
	}

	commitFiles(t, dir, "rename and edit", map[string]string{"new.go": body + "\n// edited\n"})
	t.Chdir(dir)

	p := &ChangedSinceProvider{Ref: "HEAD~1"}

	files, err := p.GetFiles(t.Context())
	if err != nil || len(files) != 1 || files[0] != "new.go" {
		t.Fatalf("GetFiles() = %v, %v; want [new.go]", files, err)
	}

	diff, err := p.GetDiff(t.Context(), "new.go")
	if err != nil || !strings.Contains(diff, "+// edited") || strings.Contains(diff, "+package a") {
		t.Fatalf("GetDiff() = %q, %v; want only the edit, not the whole renamed file as added", diff, err)
	}
}

func TestChangedSinceProvider_ListsReadsAndDiffsChangedFiles(t *testing.T) {
	dir := initRepo(t)

	commitFiles(t, dir, "base", map[string]string{"a.go": "package a\n"})
	commitFiles(t, dir, "change", map[string]string{"a.go": "package a\n// edited\n", "b.go": "package b\n"})
	t.Chdir(dir)

	p := &ChangedSinceProvider{Ref: "HEAD~1"}

	files, err := p.GetFiles(t.Context())
	if err != nil {
		t.Fatalf("GetFiles() error = %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("GetFiles() = %v, want a.go and b.go", files)
	}

	content, err := p.GetContent(t.Context(), "b.go")
	if err != nil || content != "package b\n" {
		t.Fatalf("GetContent() = %q, %v", content, err)
	}

	diff, err := p.GetDiff(t.Context(), "a.go")
	if err != nil || !strings.Contains(diff, "edited") {
		t.Fatalf("GetDiff() = %q, %v", diff, err)
	}
}

func TestMultiFileProvider_GetFiles(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{
			name:  "no duplicates",
			paths: []string{"a.go", "b.go", "c.go"},
			want:  []string{"a.go", "b.go", "c.go"},
		},
		{
			name:  "duplicate path deduplicated, first occurrence order preserved",
			paths: []string{"a.go", "b.go", "a.go"},
			want:  []string{"a.go", "b.go"},
		},
		{
			name:  "single file",
			paths: []string{"a.go"},
			want:  []string{"a.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &MultiFileProvider{Paths: tt.paths}

			got, err := p.GetFiles(t.Context())
			if err != nil {
				t.Fatalf("GetFiles() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
