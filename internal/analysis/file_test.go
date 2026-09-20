package analysis

import (
	"strings"
	"testing"
	"unicode/utf8"
)

type diffProvider struct {
	diff      string
	diffCalls int
}

func (p *diffProvider) GetFiles() ([]string, error)       { return nil, nil }
func (p *diffProvider) GetContent(string) (string, error) { return "", nil }
func (p *diffProvider) GetDiff(string) (string, error)    { p.diffCalls++; return p.diff, nil }

const sampleDiff = "diff --git a/svc.go b/svc.go\nindex 1..2 100644\n--- a/svc.go\n+++ b/svc.go\n@@ -1,2 +1,2 @@\n-old line\n+new line\n"

func TestQueryFile_PrefersTheStrippedDiffOverContent(t *testing.T) {
	f := &queryFile{path: "svc.go", content: "whole file", provider: &diffProvider{diff: sampleDiff}}

	got := f.QueryText()

	if strings.Contains(got, "diff --git") || !strings.Contains(got, "new line") {
		t.Fatalf("QueryText = %q, want the diff's code lines without patch metadata", got)
	}
}

func TestQueryFile_UpdateBaselineUsesWholeContentNotTheDiff(t *testing.T) {
	provider := &diffProvider{diff: sampleDiff}
	f := &queryFile{path: "svc.go", content: "whole file", provider: provider, updateBaseline: true}

	if got := f.QueryText(); got != "whole file" {
		t.Fatalf("QueryText = %q, want the whole file", got)
	}
	if provider.diffCalls != 0 {
		t.Fatalf("GetDiff called %d times during a baseline scan, want 0", provider.diffCalls)
	}
}

func TestQueryFile_BuildsTheTextOnce(t *testing.T) {
	provider := &diffProvider{diff: sampleDiff}
	f := &queryFile{path: "svc.go", content: "whole file", provider: provider}

	first := f.QueryText()
	second := f.QueryText()

	if first != second || provider.diffCalls != 1 {
		t.Fatalf("diffCalls = %d, want the text built once and reused", provider.diffCalls)
	}
}

func TestQueryFile_CapsAtSixThousandBytesOnALineBoundary(t *testing.T) {
	line := strings.Repeat("x", 89) + "\n"
	f := &queryFile{path: "svc.go", content: strings.Repeat(line, 100), provider: &diffProvider{}, updateBaseline: true}

	got := f.QueryText()

	if !strings.HasSuffix(got, "\n") || len(got) != 5940 {
		t.Fatalf("len = %d, want the 6000-byte cap rolled back to the last newline (5940)", len(got))
	}
}

func TestQueryFile_CapNeverSplitsARune(t *testing.T) {
	f := &queryFile{path: "svc.go", content: strings.Repeat("é", 4000), provider: &diffProvider{}, updateBaseline: true}

	got := f.QueryText()

	if len(got) > 6000 || !utf8.ValidString(got) {
		t.Fatalf("len = %d valid = %v, want a capped, valid UTF-8 string", len(got), utf8.ValidString(got))
	}
}
