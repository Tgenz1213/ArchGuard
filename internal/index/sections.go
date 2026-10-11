package index

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var errNoEmbeddableText = errors.New("no text left to embed after analysis.included_headers or analysis.exclude_headers")

// The zero value embeds an ADR's whole content.
type SectionSelection struct {
	include []string
	exclude []string
}

// NewSectionSelection ignores exclude when include names any heading, since include already leaves out every other section.
func NewSectionSelection(include, exclude []string) SectionSelection {
	if included := normalizeHeadings(include); len(included) > 0 {
		return SectionSelection{include: included}
	}

	return SectionSelection{exclude: normalizeHeadings(exclude)}
}

func normalizeHeadings(headings []string) []string {
	normalized := make([]string, 0, len(headings))
	for _, heading := range headings {
		if heading = strings.ToLower(normalizeHeading(heading)); heading != "" {
			normalized = append(normalized, heading)
		}
	}

	slices.Sort(normalized)

	return slices.Compact(normalized)
}

func (s SectionSelection) selects() bool {
	return len(s.include) > 0 || len(s.exclude) > 0
}

// Joined to the saved embedding format, so changing the selection re-embeds every ADR.
func (s SectionSelection) format() string {
	switch {
	case len(s.include) > 0:
		return fmt.Sprintf("%s included_headers=%q", embeddingFormat, s.include)
	case len(s.exclude) > 0:
		return fmt.Sprintf("%s exclude_headers=%q", embeddingFormat, s.exclude)
	default:
		return embeddingFormat
	}
}

// apply returns the selected sections of content. A section runs from its "##" heading to the next "#" or "##" heading,
// so it keeps any "###" beneath it. Text outside every "##" section is kept only when no headings are included.
func (s SectionSelection) apply(content string) string {
	if !s.selects() {
		return content
	}

	src := []byte(content)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))

	var selected strings.Builder

	keep := len(s.include) == 0
	start := 0

	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level > 2 || heading.Pos() < 0 {
			continue
		}

		if keep {
			selected.WriteString(content[start:heading.Pos()])
		}

		start = heading.Pos()
		keep = s.keeps(heading, src)
	}

	if keep {
		selected.WriteString(content[start:])
	}

	return selected.String()
}

func (s SectionSelection) keeps(heading *ast.Heading, src []byte) bool {
	if heading.Level == 1 {
		return len(s.include) == 0
	}

	name := strings.ToLower(headingText(heading, src))
	if len(s.include) > 0 {
		return slices.Contains(s.include, name)
	}

	return !slices.Contains(s.exclude, name)
}
