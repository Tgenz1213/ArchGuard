package analysis

import (
	"regexp"
	"strconv"
	"strings"
)

type diffLine struct {
	Kind   byte
	Number int
	Text   string
	Hunk   int
}

var hunkStart = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func hunksOnly(diff string) string {
	if strings.HasPrefix(diff, "@@") {
		return diff
	}

	idx := strings.Index(diff, "\n@@")
	if idx == -1 {
		return ""
	}

	return diff[idx+1:]
}

// A removed line's Number is the new-file line it sat in front of.
func parseDiff(diff string) []diffLine {
	var lines []diffLine

	next, hunk := 0, -1
	inHunk := false

	for _, raw := range strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n") {
		if m := hunkStart.FindStringSubmatch(raw); m != nil {
			start, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}

			next, inHunk = start, true
			hunk++

			continue
		}

		if !inHunk || raw == "" {
			continue
		}

		switch raw[0] {
		case '+', ' ':
			lines = append(lines, diffLine{Kind: raw[0], Number: next, Text: raw[1:], Hunk: hunk})
			next++
		case '-':
			lines = append(lines, diffLine{Kind: '-', Number: next, Text: raw[1:], Hunk: hunk})
		}
	}

	return lines
}

// diffSides returns the code before a change (context and removed lines) and after it (context and added lines), with a blank line between hunks.
func diffSides(diff string) (before, after string) {
	var beforeHunks, afterHunks [][]string

	for _, line := range parseDiff(diff) {
		if line.Kind != '+' {
			beforeHunks = appendToHunk(beforeHunks, line)
		}

		if line.Kind != '-' {
			afterHunks = appendToHunk(afterHunks, line)
		}
	}

	return joinHunks(beforeHunks), joinHunks(afterHunks)
}

// appendToHunk starts a new hunk when line belongs to a later one; hunks is indexed by diffLine.Hunk.
func appendToHunk(hunks [][]string, line diffLine) [][]string {
	for len(hunks) <= line.Hunk {
		hunks = append(hunks, nil)
	}

	hunks[line.Hunk] = append(hunks[line.Hunk], line.Text)

	return hunks
}

func joinHunks(hunks [][]string) string {
	texts := make([]string, 0, len(hunks))
	for _, lines := range hunks {
		if len(lines) > 0 {
			texts = append(texts, strings.Join(lines, "\n"))
		}
	}

	return strings.Join(texts, "\n\n")
}
