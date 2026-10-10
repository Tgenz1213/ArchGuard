package analysis

import (
	"slices"
	"testing"
)

const sampleDiffPreamble = "diff --git a/a.go b/a.go\n" +
	"index 111..222 100644\n" +
	"--- a/a.go\n" +
	"+++ b/a.go\n"

const sampleHunks = "@@ -1,5 +1,6 @@\n" +
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
		{' ', 1, "package a"},
		{'-', 2, "var old = 1"},
		{'+', 2, "var added = 1"},
		{'+', 3, "var alsoAdded = 2"},
		{' ', 4, ""},
		{' ', 5, "func keep() {}"},
		{' ', 21, "ctx"},
		{'+', 22, "++ looks like a header but is added"},
		{' ', 23, "tail"},
	}

	if !slices.Equal(got, want) {
		t.Fatalf("parseDiff() = %+v, want %+v", got, want)
	}
}
