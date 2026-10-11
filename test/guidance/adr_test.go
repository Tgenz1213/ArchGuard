package guidance

import (
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

func TestADRsFollowTheTemplate(t *testing.T) {
	want := sectionHeadings(readGuidance(t, "docs/ADR_TEMPLATE.md"))
	if len(want) == 0 {
		t.Fatal("no ## headings in docs/ADR_TEMPLATE.md")
	}

	for _, adr := range adrFiles(t) {
		t.Run(adr, func(t *testing.T) {
			content := readGuidance(t, adr)

			if got := sectionHeadings(content); !slices.Equal(got, want) {
				t.Errorf("## headings = %q, want %q", got, want)
			}

			if !hasScope(t, content) {
				t.Error("frontmatter declares no scope")
			}
		})
	}
}

func adrFiles(t *testing.T) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(repoRoot, "docs", "arch", "*.md"))
	if err != nil {
		t.Fatal(err)
	}

	if len(matches) == 0 {
		t.Fatal("no ADRs found")
	}

	files := make([]string, 0, len(matches))
	for _, match := range matches {
		files = append(files, path.Join("docs", "arch", filepath.Base(match)))
	}

	return files
}

func sectionHeadings(content string) []string {
	_, body := splitFrontmatter(content)
	src := []byte(body)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))

	var headings []string
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level != 2 {
			continue
		}

		headings = append(headings, strings.TrimSpace(string(heading.Lines().Value(src))))
	}

	return headings
}

func hasScope(t *testing.T, content string) bool {
	t.Helper()

	frontmatter, _ := splitFrontmatter(content)

	var parsed struct {
		Scope yaml.Node `yaml:"scope"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &parsed); err != nil {
		t.Fatalf("parsing frontmatter: %v", err)
	}

	switch parsed.Scope.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(parsed.Scope.Value) != ""
	case yaml.SequenceNode:
		return len(parsed.Scope.Content) > 0
	default:
		return false
	}
}

func splitFrontmatter(content string) (frontmatter, body string) {
	rest, found := strings.CutPrefix(content, "---\n")
	if !found {
		return "", content
	}

	frontmatter, body, found = strings.Cut(rest, "\n---\n")
	if !found {
		return "", content
	}

	return frontmatter, body
}
