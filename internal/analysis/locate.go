package analysis

import (
	"slices"
	"strings"
)

type placement int

const (
	placedNowhere placement = iota
	placedInAdded
	placedInRemoved
	placedInContext
	placedInFile
)

// Whole-line matches come before substring matches, and a changed line before an unchanged one.
var searchOrder = []struct {
	exact bool
	kind  byte
	where placement
}{
	{true, '+', placedInAdded},
	{true, '-', placedInRemoved},
	{true, ' ', placedInContext},
	{false, '+', placedInAdded},
	{false, '-', placedInRemoved},
	{false, ' ', placedInContext},
}

func locateQuote(lines []diffLine, quote string) (placement, int) {
	want := quoteLines(quote)
	if len(want) == 0 {
		return placedNowhere, 0
	}

	for _, try := range searchOrder {
		line, ok := matchRun(lines, want, try.kind, try.exact)
		if !ok {
			continue
		}

		if try.where == placedInContext {
			line = 0
		}

		return try.where, line
	}

	return placedNowhere, 0
}

func quoteLines(quote string) []string {
	var want []string

	for _, line := range strings.Split(strings.ReplaceAll(quote, "\r\n", "\n"), "\n") {
		if normalized := normalizeSpace(line); normalized != "" {
			want = append(want, normalized)
		}
	}

	return want
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func matchRun(lines []diffLine, want []string, kind byte, exact bool) (int, bool) {
	for start, line := range lines {
		if line.Kind == kind && runMatches(lines, start, want, exact) {
			return line.Number, true
		}
	}

	return 0, false
}

func runMatches(lines []diffLine, start int, want []string, exact bool) bool {
	at := start

	for i, w := range want {
		if i > 0 {
			at = nextOfKind(lines, at)
		}

		if at == -1 || !lineMatches(lines[at], w, len(want) == 1 && !exact) {
			return false
		}
	}

	return true
}

// Added and context lines are consecutive in the new file, so removed lines between them do not break a run.
func nextOfKind(lines []diffLine, from int) int {
	kind := lines[from].Kind
	next := from + 1

	for kind != '-' && next < len(lines) && lines[next].Kind == '-' {
		next++
	}

	if next < len(lines) && lines[next].Kind == kind {
		return next
	}

	return -1
}

// A quote that kept the diff's "+" or "-" in front of the line still matches it.
func lineMatches(line diffLine, want string, partial bool) bool {
	text := normalizeSpace(line.Text)
	if sameText(text, want, partial) {
		return true
	}

	if line.Kind == ' ' || !strings.HasPrefix(want, string(line.Kind)) {
		return false
	}

	unmarked := normalizeSpace(want[1:])

	return unmarked != "" && sameText(text, unmarked, partial)
}

func sameText(text, want string, partial bool) bool {
	if partial {
		return strings.Contains(text, want)
	}

	return text == want
}

// Only added and unchanged lines are in the file. Whole-line matches come before substring matches, and an added line before an unchanged one.
var newFileSearchOrder = []struct {
	exact     bool
	addedOnly bool
}{
	{true, true},
	{true, false},
	{false, true},
	{false, false},
}

func locateInNewFile(lines []diffLine, quote string) (placement, int) {
	want := quoteLines(quote)
	if len(want) == 0 {
		return placedNowhere, 0
	}

	newFile := slices.DeleteFunc(slices.Clone(lines), func(line diffLine) bool { return line.Kind == '-' })

	for _, try := range newFileSearchOrder {
		if line, ok := matchNewFileRun(newFile, want, try.exact, try.addedOnly); ok {
			return placedInFile, line
		}
	}

	return placedNowhere, 0
}

func matchNewFileRun(lines []diffLine, want []string, exact, addedOnly bool) (int, bool) {
	for start, line := range lines {
		if addedOnly && line.Kind != '+' {
			continue
		}

		if newFileRunMatches(lines, start, want, exact) {
			return line.Number, true
		}
	}

	return 0, false
}

func newFileRunMatches(lines []diffLine, start int, want []string, exact bool) bool {
	if start+len(want) > len(lines) {
		return false
	}

	for i, w := range want {
		at := start + i
		if i > 0 && lines[at].Number != lines[at-1].Number+1 {
			return false
		}

		if !lineMatches(lines[at], w, len(want) == 1 && !exact) {
			return false
		}
	}

	return true
}
