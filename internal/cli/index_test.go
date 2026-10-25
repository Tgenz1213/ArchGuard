package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestPrintIndexSummary_MalformedRules(t *testing.T) {
	var buf bytes.Buffer

	printIndexSummary(index.BuildIndexResult{
		Discovered: 2,
		Valid:      2,
		MalformedRules: []index.MalformedRules{
			{RelPath: "0001-a.md", Reason: "frontmatter: rules must be a list"},
			{RelPath: "0002-b.md", Reason: `"Rules" section: rule 1: bullet has no statement text`},
		},
	}, output.New(&buf, false))

	for _, want := range []string{"0001-a.md", "0002-b.md", "malformed): 2"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, buf.String())
		}
	}
}

func TestPrintIndexSummary_NoMalformedRulesSectionWhenNone(t *testing.T) {
	var buf bytes.Buffer

	printIndexSummary(index.BuildIndexResult{Discovered: 1, Valid: 1}, output.New(&buf, false))

	if strings.Contains(buf.String(), "Rules ignored") {
		t.Errorf("expected no malformed-rules section, got:\n%s", buf.String())
	}
}
