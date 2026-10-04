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
	recorded := append([]RecordedEntry(nil), report.Recorded...)
	unrecorded := append([]UnrecordedViolation(nil), report.Unrecorded...)

	sort.SliceStable(recorded, func(i, j int) bool {
		left, right := recorded[i], recorded[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.ADRID < right.ADRID
	})
	sort.SliceStable(unrecorded, func(i, j int) bool {
		left, right := unrecorded[i], unrecorded[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.ADRID < right.ADRID
	})

	if len(recorded) > 0 {
		p.Result("Recorded:")

		list := p.Indented()
		for _, entry := range recorded {
			line := "no line"
			if entry.Line > 0 {
				line = fmt.Sprintf("line %d", entry.Line)
			}

			if entry.Reason != "" {
				list.Result("%s: ADR %s %s, %s (reason: %s)", entry.File, entry.ADRID, entry.Title, line, entry.Reason)
			} else {
				list.Result("%s: ADR %s %s, %s", entry.File, entry.ADRID, entry.Title, line)
			}
		}

		p.Result("")
	}

	if len(unrecorded) > 0 {
		p.Result("Not recorded:")

		list := p.Indented()
		for _, violation := range unrecorded {
			list.Result("%s: ADR %s %s: %s", violation.File, violation.ADRID, violation.Title, violation.Reason)
		}

		p.Result("")
	}

	p.reportGaps(report.Gaps)

	summary := fmt.Sprintf("Baseline written to %s: %d recorded, %d not recorded.", report.Path, len(recorded), len(unrecorded))
	if gaps := report.gapSummary(); len(gaps) > 0 {
		summary += " Not fully checked: " + strings.Join(gaps, ", ") + "."
	}

	p.Result("%s", summary)
}
