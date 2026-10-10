package analysis

import "testing"

func TestLocateQuote(t *testing.T) {
	lines := parseDiff(sampleHunks)
	crlf := parseDiff("@@ -1 +1,2 @@\r\n context\r\n+added line\r\n")
	removed := parseDiff("@@ -3,4 +3,2 @@\n ctx\n-first removed\n-second removed\n tail\n")
	removedApart := parseDiff("@@ -1,4 +1,2 @@\n-a\n ctx\n-b\n tail\n")
	moved := parseDiff("@@ -1,2 +1,2 @@\n log(password)\n-x\n+log(password) // moved\n")
	duplicate := parseDiff("@@ -1,2 +1,3 @@\n }\n+}\n")
	crossing := parseDiff("@@ -1 +1,2 @@\n+a\n b\n")
	interleaved := parseDiff("@@ -1,2 +1,2 @@\n+a\n-x\n+b\n")

	tests := []struct {
		name  string
		lines []diffLine
		quote string
		want  placement
		line  int
	}{
		{"an added line", lines, "var added = 1", placedInAdded, 2},
		{"surrounding whitespace is ignored", lines, "  var added = 1  ", placedInAdded, 2},
		{"part of an added line", lines, "added", placedInAdded, 2},
		{"a stray leading plus", lines, "+var added = 1", placedInAdded, 2},
		{"consecutive added lines", lines, "var added = 1\nvar alsoAdded = 2", placedInAdded, 2},
		{"added lines from different hunks do not form a run", lines, "var alsoAdded = 2\n++ looks like a header but is added", placedNowhere, 0},
		{"a context line is unchanged code", lines, "func keep() {}", placedInContext, 0},
		{"a removed line is a change, placed where it was removed", lines, "var old = 1", placedInRemoved, 2},
		{"a stray leading minus", lines, "-var old = 1", placedInRemoved, 2},
		{"internal whitespace differences are ignored", lines, "func  keep()\t{}", placedInContext, 0},
		{"a whole context line beats a longer added line containing it", moved, "log(password)", placedInContext, 0},
		{"an added line beats an identical context line", duplicate, "}", placedInAdded, 2},
		{"a run does not cross from an added line into context", crossing, "a\nb", placedNowhere, 0},
		{"removed lines between added lines do not break a run", interleaved, "a\nb", placedInAdded, 1},
		{"consecutive removed lines", removed, "first removed\nsecond removed", placedInRemoved, 4},
		{"removed lines with context between do not form a run", removedApart, "a\nb", placedNowhere, 0},
		{"a quote found nowhere", lines, "something the model made up", placedNowhere, 0},
		{"an empty quote", lines, "", placedNowhere, 0},
		{"CRLF line endings", crlf, "added line", placedInAdded, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, line := locateQuote(tt.lines, tt.quote)
			if got != tt.want || line != tt.line {
				t.Fatalf("locateQuote(%q) = %v, %d; want %v, %d", tt.quote, got, line, tt.want, tt.line)
			}
		})
	}
}
