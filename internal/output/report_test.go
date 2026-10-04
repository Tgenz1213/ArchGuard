package output_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func renderReport(r output.Report) string {
	var buf bytes.Buffer

	output.New(&buf, false).Report(r)

	return buf.String()
}

func TestReportLayout(t *testing.T) {
	got := renderReport(output.Report{
		Violations: []output.Violation{
			{File: "b.go", Line: 7, Verified: true, ADRID: "0002", Title: "No globals", Reasoning: "uses a global", Code: "var x int"},
			{File: "a.go", Line: 3, Verified: true, ADRID: "0001", Title: "Use Go", Reasoning: "not Go", Code: "import py", Suggestion: "rewrite it"},
			{File: "a.go", Verified: false, ADRID: "0003", Title: "Quote it", Reasoning: "vague"},
		},
		SkippedFiles: []output.FileGap{{File: "c.go", Reason: "reading file: boom"}},
		FailedChecks: []output.FailedCheck{{File: "a.go", ADRID: "0004", Title: "Flaky", Reason: "LLM down"}},
		FailedStages: []output.FailedStage{{Stage: "rerank", File: "d.go", Reason: "unavailable"}},
		Baselined:    2,
	})

	want := `Violations:
a.go
  [VIOLATION] 0003 Quote it [UNVERIFIED: quoted code not found in analyzed content]
    Reasoning: vague
  [VIOLATION] 0001 Use Go [Line 3]
    Reasoning: not Go
    Code: import py
    Suggestion (unverified): rewrite it
b.go
  [VIOLATION] 0002 No globals [Line 7]
    Reasoning: uses a global
    Code: var x int

Skipped files:
  c.go: reading file: boom

Failed ADR checks:
  a.go: ADR 0004 Flaky: LLM down

Failed stages:
  d.go: stage rerank: unavailable

3 new violation(s) in 2 file(s), 2 baselined. Not fully checked: 1 file(s) skipped, 1 ADR check(s) failed, 1 stage failure(s).
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReportWithNothingToReportIsOneLine(t *testing.T) {
	tests := []struct {
		name   string
		report output.Report
		want   string
	}{
		{"nothing", output.Report{}, "No new architectural violations found.\n"},
		{"only baselined", output.Report{Baselined: 3}, "No new architectural violations found (3 baselined).\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderReport(tt.report); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReportCoverageGapsAloneAreNotReportedAsClean(t *testing.T) {
	got := renderReport(output.Report{SkippedFiles: []output.FileGap{{File: "x.go", Reason: "r"}}})

	if strings.Contains(got, "No new architectural violations found") {
		t.Errorf("a run with a skipped file reported as clean:\n%s", got)
	}

	if !strings.Contains(got, "x.go: r") || !strings.Contains(got, "0 new violation(s) in 0 file(s), 0 baselined. Not fully checked: 1 file(s) skipped.") {
		t.Errorf("skip or totals missing:\n%s", got)
	}
}

func TestReportSummaryCountsSkippedFilesAndFailedChecksSeparately(t *testing.T) {
	got := renderReport(output.Report{
		SkippedFiles: []output.FileGap{{File: "x.go", Reason: "r"}},
		FailedChecks: []output.FailedCheck{{File: "x.go", ADRID: "1", Title: "t", Reason: "r"}, {File: "y.go", ADRID: "2", Title: "t", Reason: "r"}},
	})

	if !strings.Contains(got, "Not fully checked: 1 file(s) skipped, 2 ADR check(s) failed.") {
		t.Errorf("summary merged or miscounted the gaps:\n%s", got)
	}
}

func TestReportSummaryOmitsGapsWhenNothingWasMissed(t *testing.T) {
	got := renderReport(output.Report{Violations: []output.Violation{{File: "a.go", Line: 1, Verified: true, ADRID: "1", Title: "t"}}})

	if strings.Contains(got, "Not fully checked") {
		t.Errorf("summary mentioned gaps for a fully checked run:\n%s", got)
	}
}

func TestReportWriteFailureIsPrimaryOutputFailure(t *testing.T) {
	p := output.New(failingWriter{}, false)

	p.Report(output.Report{Violations: []output.Violation{{File: "a.go", Verified: true, ADRID: "1", Title: "t"}}})

	if p.Err() == nil {
		t.Error("a failed report write was not recorded")
	}
}

func TestReportOrderDoesNotDependOnInputOrder(t *testing.T) {
	violations := []output.Violation{
		{File: "a.go", Line: 9, Verified: true, ADRID: "2", Title: "t"},
		{File: "a.go", Line: 2, Verified: true, ADRID: "1", Title: "t"},
		{File: "b.go", Line: 1, Verified: true, ADRID: "1", Title: "t"},
	}
	skipped := []output.FileGap{{File: "z.go", Reason: "r"}, {File: "c.go", Reason: "r"}}
	failed := []output.FailedCheck{{File: "c.go", ADRID: "9", Title: "t", Reason: "r"}, {File: "c.go", ADRID: "3", Title: "t", Reason: "r"}}

	want := renderReport(output.Report{Violations: violations, SkippedFiles: skipped, FailedChecks: failed})

	slices.Reverse(violations)
	slices.Reverse(skipped)
	slices.Reverse(failed)

	if got := renderReport(output.Report{Violations: violations, SkippedFiles: skipped, FailedChecks: failed}); got != want {
		t.Errorf("reordered input changed the report:\n%s\nvs\n%s", got, want)
	}
}

func TestReportOmitsEmptyCodeAndSuggestion(t *testing.T) {
	got := renderReport(output.Report{Violations: []output.Violation{{File: "a.go", Line: 1, Verified: true, ADRID: "1", Title: "t", Reasoning: "r"}}})

	if strings.Contains(got, "Code:") || strings.Contains(got, "Suggestion") {
		t.Errorf("empty code or suggestion was printed:\n%s", got)
	}
}

func TestReportColorNeverSpansLines(t *testing.T) {
	var buf bytes.Buffer

	output.New(&buf, false, output.WithColor(true)).Report(output.Report{
		Violations: []output.Violation{{File: "a.go", Line: 1, Verified: true, ADRID: "1", Title: "t", Reasoning: "one\ntwo", Code: "x\ny"}},
	})

	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "\x1b[") && !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line leaves a color open: %q", line)
		}
	}
}
