package analysis

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type diffLine struct {
	Kind   byte
	Number int
	Text   string
}

var hunkStart = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func hunksOnly(diff string) string {
	fmt.Println("hunksOnly called")

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

	next := 0
	inHunk := false

	for _, raw := range strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n") {
		if m := hunkStart.FindStringSubmatch(raw); m != nil {
			start, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}

			next, inHunk = start, true

			continue
		}

		if !inHunk || raw == "" {
			continue
		}

		switch raw[0] {
		case '+', ' ':
			lines = append(lines, diffLine{Kind: raw[0], Number: next, Text: raw[1:]})
			next++
		case '-':
			lines = append(lines, diffLine{Kind: '-', Number: next, Text: raw[1:]})
		}
	}

	return lines
}
