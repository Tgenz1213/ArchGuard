package analysis

import "strings"

type placement int

const (
	placedNowhere placement = iota
	placedInAdded
	placedInRemoved
	placedInContext
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
