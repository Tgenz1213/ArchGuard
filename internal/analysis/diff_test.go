package analysis

import (
	"slices"
	"testing"
)

const (
	sampleDiffPreamble = "diff --git a/a.go b/a.go\n" +
		"index 111..222 100644\n" +
		"--- a/a.go\n" +
		"+++ b/a.go\n"

	sampleHunks = "@@ -1,5 +1,6 @@\n" +
		" package a\n" +
		"-var old = 1\n" +
		"+var added = 1\n" +
		"+var alsoAdded = 2\n" +
		" \n" +
		" func keep() {}\n" +
		"@@ -20,2 +21,3 @@\n" +
		" ctx\n" +
		"+++ looks like a header but is added\n" +
		" tail\n" +
		"\\ No newline at end of file\n"
)

func TestHunksOnly(t *testing.T) {
	tests := []struct{ name, diff, want string }{
		{"strips the preamble", sampleDiffPreamble + sampleHunks, sampleHunks},
		{"a diff that starts at a hunk is unchanged", "@@ -1 +1 @@\n-a\n+b\n", "@@ -1 +1 @@\n-a\n+b\n"},
		{"a binary diff has no hunks", "diff --git a/x b/x\nBinary files a/x and b/x differ\n", ""},
		{"an empty diff has no hunks", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hunksOnly(tt.diff); got != tt.want {
				t.Fatalf("hunksOnly() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDiff_NumbersAndKinds(t *testing.T) {
	got := parseDiff(sampleHunks)
	want := []diffLine{
		{' ', 1, "package a", 0},
		{'-', 2, "var old = 1", 0},
		{'+', 2, "var added = 1", 0},
		{'+', 3, "var alsoAdded = 2", 0},
		{' ', 4, "", 0},
		{' ', 5, "func keep() {}", 0},
		{' ', 21, "ctx", 1},
		{'+', 22, "++ looks like a header but is added", 1},
		{' ', 23, "tail", 1},
	}

	if !slices.Equal(got, want) {
		t.Fatalf("parseDiff() = %+v, want %+v", got, want)
	}
}

func TestDiffSides(t *testing.T) {
	tests := []struct {
		name, diff, before, after string
	}{
		{
			name:   "mixed hunks keep context on both sides and a blank line between hunks",
			diff:   sampleHunks,
			before: "package a\nvar old = 1\n\nfunc keep() {}\n\nctx\ntail",
			after:  "package a\nvar added = 1\nvar alsoAdded = 2\n\nfunc keep() {}\n\nctx\n++ looks like a header but is added\ntail",
		},
		{
			name:   "a hunk that only removes lines has no after side",
			diff:   "@@ -1,2 +0,0 @@\n-withGate()\n-route()\n",
			before: "withGate()\nroute()",
		},
		{
			name:  "a new file has no before side",
			diff:  "@@ -0,0 +1,2 @@\n+package a\n+var x = 1\n",
			after: "package a\nvar x = 1",
		},
		{
			name:   "a removal-only hunk leaves the other hunk's after side alone",
			diff:   "@@ -1,1 +0,0 @@\n-gone()\n@@ -9,0 +9,1 @@\n+added()\n",
			before: "gone()",
			after:  "added()",
		},
		{
			name:   "a truncated diff keeps what it has",
			diff:   "@@ -1,3 +1,3 @@\n keep\n-old\n+ne",
			before: "keep\nold",
			after:  "keep\nne",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after := diffSides(tt.diff)
			if before != tt.before || after != tt.after {
				t.Errorf("diffSides() = (%q, %q), want (%q, %q)", before, after, tt.before, tt.after)
			}
		})
	}
}
