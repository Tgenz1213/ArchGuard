package output

import (
	"fmt"
	"sort"
	"strings"
)

type Violation struct {
	File       string
	Line       int
	Verified   bool
	ADRID      string
	Title      string
	Reasoning  string
	Code       string
	Suggestion string
}

type Report struct {
	Violations []Violation
	Baselined  int
	Gaps
}

func sortedViolations(violations []Violation) []Violation {
	sorted := append([]Violation(nil), violations...)

	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if left.File != right.File {
			return left.File < right.File
		}

		if left.Line != right.Line {
			return left.Line < right.Line
		}

		return left.ADRID < right.ADRID
	})

	return sorted
}

func violatingFiles(violations []Violation) []string {
	seen := map[string]struct{}{}

	var files []string

	for _, violation := range violations {
		if _, ok := seen[violation.File]; !ok {
			seen[violation.File] = struct{}{}
			files = append(files, violation.File)
		}
	}

	sort.Strings(files)

	return files
}

func (p *Printer) Report(report Report) {
	report.Violations = sortedViolations(report.Violations)
	report.Gaps = report.sortedGaps()
	files := violatingFiles(report.Violations)
	gaps := report.gapSummary()

	if len(files) == 0 && len(gaps) == 0 {
		if report.Baselined > 0 {
			p.Result("No new architectural violations found (%d baselined).", report.Baselined)
			return
		}

		p.Result("No new architectural violations found.")

		return
	}

	if len(files) > 0 {
		p.Result("Violations:")
		p.reportViolations(report.Violations, files)
		p.Result("")
	}

	p.reportGaps(report.Gaps)

	summary := fmt.Sprintf("%d new violation(s) in %d file(s), %d baselined.", len(report.Violations), len(files), report.Baselined)
	if len(gaps) > 0 {
		summary += " Not fully checked: " + strings.Join(gaps, ", ") + "."
	}

	p.Result("%s", summary)
}

func (p *Printer) reportViolations(violations []Violation, files []string) {
	for _, file := range files {
		fileGroup := p.Group(file)

		for _, violation := range violations {
			if violation.File != file {
				continue
			}

			header := fmt.Sprintf("[VIOLATION] %s %s [Line %d]", violation.ADRID, violation.Title, violation.Line)
			if !violation.Verified {
				header = fmt.Sprintf("[VIOLATION] %s %s [UNVERIFIED: quoted code not found in analyzed content]", violation.ADRID, violation.Title)
			}

			details := fileGroup.group(header, violationStyle)
			details.write("", false, true)
			details.Field("Reasoning", "%s", violation.Reasoning)

			if violation.Code != "" {
				details.Field("Code", "%s", violation.Code)
			}

			if violation.Suggestion != "" {
				details.Field("Suggestion (unverified)", "%s", violation.Suggestion)
			}

			details.Flush()
		}

		fileGroup.Flush()
	}
}
