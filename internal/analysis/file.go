package analysis

type queryFile struct {
	path           string
	content        string
	provider       ContentProvider
	updateBaseline bool
	text           string
	built          bool
}

func (f *queryFile) Path() string { return f.path }

func (f *queryFile) QueryText() string {
	if f.built {
		return f.text
	}
	f.built = true

	// A diff only covers the uncommitted hunk, not the whole file --update-baseline needs.
	text := f.content
	if !f.updateBaseline {
		if diff, err := f.provider.GetDiff(f.path); err == nil && diff != "" {
			text = stripDiffMetadata(diff)
		}
	}
	if len(text) > 6000 {
		text = rollBackToNewline(truncateRuneSafe(text, 6000))
	}
	f.text = text
	return f.text
}
