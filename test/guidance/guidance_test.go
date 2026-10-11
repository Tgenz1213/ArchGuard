package guidance

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const repoRoot = "../.."

var (
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// Only backticked paths ending in .md are checked; other backticked text is code or a glob.
	backtickedDoc = regexp.MustCompile("`([^`\\s*<>]+\\.md)`")
)

func TestRulesPathsMatchTrackedFiles(t *testing.T) {
	tracked := trackedFiles(t)

	for _, rulesFile := range rulesFiles(t) {
		t.Run(rulesFile, func(t *testing.T) {
			globs := pathsFrontmatter(t, readGuidance(t, rulesFile))
			if len(globs) == 0 {
				t.Fatalf("no paths in frontmatter")
			}

			for _, glob := range globs {
				if !matchesAny(glob, tracked) {
					t.Errorf("paths glob %q matches no tracked file", glob)
				}
			}
		})
	}
}

func TestGuidanceLinksResolve(t *testing.T) {
	files := append([]string{"CLAUDE.md"}, rulesFiles(t)...)

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			text := readGuidance(t, file)

			for _, link := range markdownLinks(text) {
				assertExists(t, path.Join(path.Dir(file), link), link)
			}

			for _, doc := range backtickedDocs(text) {
				assertExists(t, doc, doc)
			}
		})
	}
}

func trackedFiles(t *testing.T) []string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z")
	cmd.Dir = repoRoot

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

func rulesFiles(t *testing.T) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(repoRoot, ".claude", "rules", "*.md"))
	if err != nil {
		t.Fatal(err)
	}

	if len(matches) == 0 {
		t.Fatal("no rules files found")
	}

	files := make([]string, 0, len(matches))
	for _, match := range matches {
		files = append(files, path.Join(".claude", "rules", filepath.Base(match)))
	}

	return files
}

func readGuidance(t *testing.T, file string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}

	return string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
}

func pathsFrontmatter(t *testing.T, text string) []string {
	t.Helper()

	body, found := strings.CutPrefix(text, "---\n")
	if !found {
		return nil
	}

	frontmatter, _, found := strings.Cut(body, "\n---\n")
	if !found {
		t.Fatal("frontmatter is not closed")
	}

	var parsed struct {
		Paths []string `yaml:"paths"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &parsed); err != nil {
		t.Fatalf("parsing frontmatter: %v", err)
	}

	return parsed.Paths
}

func matchesAny(glob string, files []string) bool {
	for _, file := range files {
		if doublestar.MatchUnvalidated(glob, file) {
			return true
		}
	}

	return false
}

func markdownLinks(text string) []string {
	var links []string

	for _, match := range markdownLink.FindAllStringSubmatch(text, -1) {
		target, _, _ := strings.Cut(match[1], "#")
		if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}

		links = append(links, target)
	}

	return links
}

func backtickedDocs(text string) []string {
	var docs []string

	for _, match := range backtickedDoc.FindAllStringSubmatch(text, -1) {
		docs = append(docs, match[1])
	}

	return docs
}

func assertExists(t *testing.T, fromRoot, written string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(fromRoot))); err != nil {
		t.Errorf("%q does not resolve: %v", written, err)
	}
}
