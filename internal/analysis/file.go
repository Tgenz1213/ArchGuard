package analysis

import "context"

type queryFile struct {
	path           string
	content        string
	provider       ContentProvider
	updateBaseline bool
	text           string
	built          bool
	readSides      func() ([]string, error)
	sides          []string
	sidesErr       error
	sidesRead      bool
}

func (f *queryFile) Path() string { return f.path }

func (f *queryFile) Sides() ([]string, error) {
	if f.sidesRead {
		return f.sides, f.sidesErr
	}

	f.sidesRead = true

	if f.readSides == nil {
		f.sides = []string{f.content}
	} else {
		f.sides, f.sidesErr = f.readSides()
	}

	return f.sides, f.sidesErr
}

func sidesOf(content string, isDiff bool) []string {
	if !isDiff {
		return []string{content}
	}

	before, after := diffSides(content)

	return []string{before, after}
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
