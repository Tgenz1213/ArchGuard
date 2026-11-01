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
			{File: "b.go", Line: 7, ADRID: "0002", Title: "No globals", Reasoning: "uses a global", Code: "var x int"},
			{File: "a.go", Line: 3, ADRID: "0001", Title: "Use Go", Reasoning: "not Go", Code: "import py", Suggestion: "rewrite it"},
		},
		Unverified: []output.Violation{
			{File: "c.go", ADRID: "0005", Title: "Cite it", Reasoning: "invented", Code: "not in the file"},
			{File: "a.go", ADRID: "0003", Title: "Quote it", Reasoning: "vague"},
		},
		SkippedFiles:  []output.FileGap{{File: "c.go", Reason: "reading file: boom"}},
		FailedChecks:  []output.FailedCheck{{File: "a.go", ADRID: "0004", Title: "Flaky", Reason: "LLM down"}},
		FailedStages:  []output.FailedStage{{Stage: "rerank", File: "d.go", Reason: "unavailable"}},
		UnindexedADRs: []output.ADRGap{{ADR: "0007-long.md", Reason: "failed to embed: boom"}},
		Baselined:     2,
		Coverage:      output.Coverage{FilesJudged: 2, ADRChecks: 5, FilesWithoutADR: 1},
	})

	want := `Violations:
a.go
  [VIOLATION] 0001 Use Go [Line 3]
    Reasoning: not Go
    Code: import py
    Suggestion (unverified): rewrite it
b.go
  [VIOLATION] 0002 No globals [Line 7]
    Reasoning: uses a global
    Code: var x int

Unverified findings (the quoted code was not found in what the model read, so they do not fail the run):
a.go
  [UNVERIFIED] 0003 Quote it
    Reasoning: vague
c.go
  [UNVERIFIED] 0005 Cite it
    Reasoning: invented
    Code: not in the file

Skipped files:
  c.go: reading file: boom

Failed ADR checks:
  a.go: ADR 0004 Flaky: LLM down

Failed nodes:
  d.go: node rerank: unavailable

ADRs not in the index (no file was checked against them):
  0007-long.md: failed to embed: boom

Checked 2 file(s) against 5 ADR check(s); 1 file(s) had no relevant ADR.
2 new violation(s) in 2 file(s), 2 baselined, 2 unverified. Not fully checked: 1 file(s) skipped, 1 ADR check(s) failed, 1 node failure(s), 1 ADR(s) not indexed.
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReportWithNothingToReportStatesCoverageThenTheResult(t *testing.T) {
	tests := []struct {
		name   string
		report output.Report
		want   string
	}{
		{"clean", output.Report{Coverage: output.Coverage{FilesJudged: 4, ADRChecks: 6, FilesWithoutADR: 2}}, "Checked 4 file(s) against 6 ADR check(s); 2 file(s) had no relevant ADR.\nNo new architectural violations found.\n"},
		{"only baselined", output.Report{Baselined: 3, Coverage: output.Coverage{FilesJudged: 1, ADRChecks: 1}}, "Checked 1 file(s) against 1 ADR check(s); 0 file(s) had no relevant ADR.\nNo new architectural violations found (3 baselined).\n"},
		{"nothing judged", output.Report{Coverage: output.Coverage{FilesWithoutADR: 12}}, "Checked 0 file(s) against 0 ADR check(s); 12 file(s) had no relevant ADR.\nNo new architectural violations found, but no file was checked against an ADR.\n"},
		{"some below the threshold", output.Report{Coverage: output.Coverage{FilesJudged: 2, ADRChecks: 3, FilesWithoutADR: 5, FilesBelowThreshold: 4}}, "Checked 2 file(s) against 3 ADR check(s); 5 file(s) had no relevant ADR, 4 of them because every candidate scored below the threshold.\nNo new architectural violations found.\n"},
		{"nothing judged because a node dropped everything", output.Report{Coverage: output.Coverage{FilesWithoutADR: 3, FilesScreenedOut: 3, ScreenedOutBy: []string{"breaks-a-rule"}}}, "Checked 0 file(s) against 0 ADR check(s); 3 file(s) had no relevant ADR, 3 of them because the breaks-a-rule node dropped every candidate.\nNo new architectural violations found, but no file was checked against an ADR: the breaks-a-rule node dropped every candidate. Run with --debug to see the scores.\n"},
		{"some below the threshold and some screened out", output.Report{Coverage: output.Coverage{FilesJudged: 1, ADRChecks: 1, FilesWithoutADR: 3, FilesBelowThreshold: 2, FilesScreenedOut: 1, ScreenedOutBy: []string{"breaks-a-rule"}}}, "Checked 1 file(s) against 1 ADR check(s); 3 file(s) had no relevant ADR, 2 of them because every candidate scored below the threshold, 1 because the breaks-a-rule node dropped every candidate.\nNo new architectural violations found.\n"},
		{"files screened out by two nodes", output.Report{Coverage: output.Coverage{FilesJudged: 1, ADRChecks: 1, FilesWithoutADR: 2, FilesScreenedOut: 2, ScreenedOutBy: []string{"screen", "strict"}}}, "Checked 1 file(s) against 1 ADR check(s); 2 file(s) had no relevant ADR, 2 of them because a node (screen, strict) dropped every candidate.\nNo new architectural violations found.\n"},
		{"nothing judged because of the threshold", output.Report{Coverage: output.Coverage{FilesWithoutADR: 3, FilesBelowThreshold: 2}}, "Checked 0 file(s) against 0 ADR check(s); 3 file(s) had no relevant ADR, 2 of them because every candidate scored below the threshold.\nNo new architectural violations found, but no file was checked against an ADR: every candidate scored below the threshold. The right threshold depends on the embedding model; run with --debug to see the scores.\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderReport(tt.report); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReportWithOnlyUnverifiedFindingsIsNotReportedAsClean(t *testing.T) {
	got := renderReport(output.Report{
		Unverified: []output.Violation{{File: "a.go", ADRID: "1", Title: "t", Reasoning: "r"}},
		Coverage:   output.Coverage{FilesJudged: 1, ADRChecks: 1},
	})

	want := `Unverified findings (the quoted code was not found in what the model read, so they do not fail the run):
a.go
  [UNVERIFIED] 1 t
    Reasoning: r

Checked 1 file(s) against 1 ADR check(s); 0 file(s) had no relevant ADR.
0 new violation(s) in 0 file(s), 0 baselined, 1 unverified.
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReportWithGapsStatesCoverageBeforeTheSummary(t *testing.T) {
	got := renderReport(output.Report{
		SkippedFiles: []output.FileGap{{File: "x.go", Reason: "r"}},
		Coverage:     output.Coverage{FilesJudged: 3, ADRChecks: 4},
	})

	want := "Checked 3 file(s) against 4 ADR check(s); 0 file(s) had no relevant ADR.\n0 new violation(s)"
	if !strings.Contains(got, want) {
		t.Errorf("coverage line missing or misplaced:\n%s", got)
	}
}

func TestReportWithGapsAndNothingJudgedStatesZeroCoverage(t *testing.T) {
	got := renderReport(output.Report{
		SkippedFiles: []output.FileGap{{File: "x.go", Reason: "r"}},
		Coverage:     output.Coverage{FilesWithoutADR: 2},
	})

	if !strings.Contains(got, "Checked 0 file(s) against 0 ADR check(s); 2 file(s) had no relevant ADR.\n") {
		t.Errorf("coverage line missing:\n%s", got)
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
	got := renderReport(output.Report{Violations: []output.Violation{{File: "a.go", Line: 1, ADRID: "1", Title: "t"}}})

	if strings.Contains(got, "Not fully checked") {
		t.Errorf("summary mentioned gaps for a fully checked run:\n%s", got)
	}
}

func TestReportWriteFailureIsPrimaryOutputFailure(t *testing.T) {
	p := output.New(failingWriter{}, false)

	p.Report(output.Report{Violations: []output.Violation{{File: "a.go", ADRID: "1", Title: "t"}}})

	if p.Err() == nil {
		t.Error("a failed report write was not recorded")
	}
}

func TestReportOrderDoesNotDependOnInputOrder(t *testing.T) {
	violations := []output.Violation{
		{File: "a.go", Line: 9, ADRID: "2", Title: "t"},
		{File: "a.go", Line: 2, ADRID: "1", Title: "t"},
		{File: "b.go", Line: 1, ADRID: "1", Title: "t"},
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
	got := renderReport(output.Report{Violations: []output.Violation{{File: "a.go", Line: 1, ADRID: "1", Title: "t", Reasoning: "r"}}})

	if strings.Contains(got, "Code:") || strings.Contains(got, "Suggestion") {
		t.Errorf("empty code or suggestion was printed:\n%s", got)
	}
}

func TestReportColorNeverSpansLines(t *testing.T) {
	var buf bytes.Buffer

	output.New(&buf, false, output.WithColor(true)).Report(output.Report{
		Violations: []output.Violation{{File: "a.go", Line: 1, ADRID: "1", Title: "t", Reasoning: "one\ntwo", Code: "x\ny"}},
	})

	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "\x1b[") && !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line leaves a color open: %q", line)
		}
	}
}
