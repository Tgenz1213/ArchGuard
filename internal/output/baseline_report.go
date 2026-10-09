package output

import (
	"fmt"
	"sort"
	"strings"
)

type RecordedEntry struct {
	File   string
	ADRID  string
	Title  string
	Line   int
	Reason string
}

type UnrecordedViolation struct {
	File   string
	ADRID  string
	Title  string
	Reason string
}

type BaselineReport struct {
	Path       string
	Recorded   []RecordedEntry
	Unrecorded []UnrecordedViolation
	Gaps
}

func (p *Printer) BaselineReport(report BaselineReport) {
	report.Gaps = report.sortedGaps()
	recorded := sortedRecorded(report.Recorded)
	unrecorded := sortedUnrecorded(report.Unrecorded)

	p.printRecorded(recorded)
	p.printUnrecorded(unrecorded)
	p.reportGaps(report.Gaps)
	p.Result("%s", report.summary(len(recorded), len(unrecorded)))
}

func sortedRecorded(entries []RecordedEntry) []RecordedEntry {
	sorted := append([]RecordedEntry(nil), entries...)

	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.ADRID < right.ADRID
	})

	return sorted
}

func sortedUnrecorded(violations []UnrecordedViolation) []UnrecordedViolation {
	sorted := append([]UnrecordedViolation(nil), violations...)

	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.ADRID < right.ADRID
	})

	return sorted
}

func (p *Printer) printRecorded(entries []RecordedEntry) {
	if len(entries) == 0 {
		return
	}

	p.Result("Recorded:")

	list := p.Indented()
	for _, entry := range entries {
		list.Result("%s", recordedLine(entry))
	}

	p.Result("")
}

func recordedLine(entry RecordedEntry) string {
	line := "no line"
	if entry.Line > 0 {
		line = fmt.Sprintf("line %d", entry.Line)
	}

	text := fmt.Sprintf("%s: ADR %s %s, %s", entry.File, entry.ADRID, entry.Title, line)
	if entry.Reason != "" {
		text += fmt.Sprintf(" (reason: %s)", entry.Reason)
	}

	return text
}

func (p *Printer) printUnrecorded(violations []UnrecordedViolation) {
	if len(violations) == 0 {
		return
	}

	p.Result("Not recorded:")

	list := p.Indented()
	for _, violation := range violations {
		list.Result("%s: ADR %s %s: %s", violation.File, violation.ADRID, violation.Title, violation.Reason)
	}

	p.Result("")
}

func (r BaselineReport) summary(recorded, unrecorded int) string {
	summary := fmt.Sprintf("Baseline written to %s: %d recorded, %d not recorded.", r.Path, recorded, unrecorded)
	if gaps := r.gapSummary(); len(gaps) > 0 {
		summary += " Not fully checked: " + strings.Join(gaps, ", ") + "."
	}

	return summary
}
