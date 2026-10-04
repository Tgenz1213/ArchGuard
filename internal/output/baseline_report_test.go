package output_test

import (
	"bytes"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func renderBaselineReport(report output.BaselineReport) string {
	var buf bytes.Buffer

	output.New(&buf, false).BaselineReport(report)

	return buf.String()
}

func TestBaselineReportLayout(t *testing.T) {
	got := renderBaselineReport(output.BaselineReport{
		Path: "archguard-baseline.json",
		Recorded: []output.RecordedEntry{
			{File: "b.go", ADRID: "0002", Title: "No globals", Line: 7},
			{File: "a.go", ADRID: "0001", Title: "Use Go", Line: 3, Reason: "accepted-debt"},
			{File: "a.go", ADRID: "0003", Title: "Whole file"},
		},
		Unrecorded: []output.UnrecordedViolation{{File: "c.go", ADRID: "0004", Title: "Quote it", Reason: "quote not in file"}},
		Gaps: output.Gaps{
			SkippedFiles: []output.FileGap{{File: "d.go", Reason: "reading file: boom"}},
			PartialFiles: []output.FileGap{{File: "e.go", Reason: "too large"}},
			FailedChecks: []output.FailedCheck{{File: "a.go", ADRID: "0005", Title: "Flaky", Reason: "LLM down"}},
		},
	})

	want := `Recorded:
  a.go: ADR 0001 Use Go, line 3 (reason: accepted-debt)
  a.go: ADR 0003 Whole file, no line
  b.go: ADR 0002 No globals, line 7

Not recorded:
  c.go: ADR 0004 Quote it: quote not in file

Skipped files:
  d.go: reading file: boom

Partly checked files:
  e.go: too large

Failed ADR checks:
  a.go: ADR 0005 Flaky: LLM down

Baseline written to archguard-baseline.json: 3 recorded, 1 not recorded. Not fully checked: 1 file(s) skipped, 1 file(s) partly checked, 1 ADR check(s) failed.
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBaselineReportWithNothingRecordedStillEndsWithTotals(t *testing.T) {
	want := "Baseline written to archguard-baseline.json: 0 recorded, 0 not recorded.\n"

	if got := renderBaselineReport(output.BaselineReport{Path: "archguard-baseline.json"}); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBaselineReportWriteFailureIsPrimaryOutputFailure(t *testing.T) {
	p := output.New(failingWriter{}, false)

	p.BaselineReport(output.BaselineReport{Path: "archguard-baseline.json"})

	if p.Err() == nil {
		t.Error("a failed baseline report write was not recorded")
	}
}
