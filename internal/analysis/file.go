package analysis

import "context"

type queryFile struct {
	path    string
	content string
	isDiff  bool
	// sideContent, when set, gives what Sides reads in place of content, worked out on first use.
	sideContent    func() (content string, isDiff bool)
	sides          []string
	provider       ContentProvider
	updateBaseline bool
	text           string
	built          bool
}

func (f *queryFile) Path() string { return f.path }

func (f *queryFile) Sides() []string {
	if f.sides != nil {
		return f.sides
	}

	content, isDiff := f.content, f.isDiff
	if f.sideContent != nil {
		content, isDiff = f.sideContent()
	}

	if !isDiff {
		f.sides = []string{content}
		return f.sides
	}

	before, after := diffSides(content)
	f.sides = []string{before, after}

	return f.sides
}

func (f *queryFile) QueryText(ctx context.Context) string {
	if f.built {
		return f.text
	}

	f.built = true

	// A diff only covers the uncommitted hunk, not the whole file --update-baseline needs.
	text := f.content
	if !f.updateBaseline {
		if diff, err := f.provider.GetDiff(ctx, f.path); err == nil && diff != "" {
			text = stripDiffMetadata(diff)
		}
	}

	if len(text) > 6000 {
		text = rollBackToNewline(truncateRuneSafe(text, 6000))
	}

	f.text = text
	return f.text
}
