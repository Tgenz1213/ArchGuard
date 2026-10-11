package index

import (
	"testing"
)

const sectionedADR = `# Use widgets

Intro before any section.

## Context

Why widgets.

## Decision

Use widgets.

### Detail

Widgets everywhere.

## Rules:

- Code MUST use widgets.

` + "```" + `
## Not a heading
` + "```" + `
`

func TestSectionSelection_Apply(t *testing.T) {
	tests := []struct {
		name      string
		selection SectionSelection
		want      string
	}{
		{
			name:      "nothing selected keeps the whole content",
			selection: NewSectionSelection(nil, nil),
			want:      sectionedADR,
		},
		{
			name:      "included headers keep only those sections and their subsections",
			selection: NewSectionSelection([]string{"decision", "Rules"}, nil),
			want: "## Decision\n\nUse widgets.\n\n### Detail\n\nWidgets everywhere.\n\n" +
				"## Rules:\n\n- Code MUST use widgets.\n\n```\n## Not a heading\n```\n",
		},
		{
			name:      "excluded headers drop those sections and keep the rest",
			selection: NewSectionSelection(nil, []string{"Context", "Rules"}),
			want: "# Use widgets\n\nIntro before any section.\n\n" +
				"## Decision\n\nUse widgets.\n\n### Detail\n\nWidgets everywhere.\n\n",
		},
		{
			name:      "included headers win over excluded ones",
			selection: NewSectionSelection([]string{"Context"}, []string{"Context"}),
			want:      "## Context\n\nWhy widgets.\n\n",
		},
		{
			name:      "an included header no section has leaves nothing",
			selection: NewSectionSelection([]string{"Consequences"}, nil),
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.selection.apply(sectionedADR); got != tt.want {
				t.Errorf("apply() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSectionSelection_Format(t *testing.T) {
	if got := NewSectionSelection(nil, nil).format(); got != embeddingFormat {
		t.Errorf("format with nothing selected = %q, want %q", got, embeddingFormat)
	}

	included := NewSectionSelection([]string{"Rules", "Decision"}, nil).format()
	excluded := NewSectionSelection(nil, []string{"Rules", "Decision"}).format()

	if included == embeddingFormat || excluded == embeddingFormat || included == excluded {
		t.Errorf("formats must differ: none %q, included %q, excluded %q", embeddingFormat, included, excluded)
	}

	if same := NewSectionSelection([]string{"decision:", " RULES"}, nil).format(); same != included {
		t.Errorf("format changed with only the case, order or a trailing colon: %q, want %q", same, included)
	}
}
