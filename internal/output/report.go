package output

import (
	"fmt"
	"sort"
	"strings"
)

type Violation struct {
	File       string
	Line       int
	ADRID      string
	Title      string
	Reasoning  string
	Code       string
	Suggestion string
}

type Report struct {
	Violations []Violation
	Unverified []Violation
	Baselined  int
	Coverage   Coverage
	Usage      *Usage
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
	report.Unverified = sortedViolations(report.Unverified)
	report.Gaps = report.sortedGaps()
	files := violatingFiles(report.Violations)
	gaps := report.gapSummary()

	if len(files) == 0 && len(report.Unverified) == 0 && len(gaps) == 0 {
		p.Result("%s", report.Coverage.line())
		p.Result("%s", report.Coverage.cleanResult(report.Baselined))
		p.reportUsage(report.Usage)

		return
	}

	if len(files) > 0 {
		p.Result("Violations:")
		p.reportFindings(report.Violations, violationHeader, violationStyle)
		p.Result("")
	}

	if len(report.Unverified) > 0 {
		p.Result("Unverified findings (the quoted code was not found in what the model read, so they do not fail the run):")
		p.reportFindings(report.Unverified, unverifiedHeader, warnStyle)
		p.Result("")
	}

	p.reportGaps(report.Gaps)
	p.Result("%s", report.Coverage.line())
	p.Result("%s", report.summary(len(files), gaps))
	p.reportUsage(report.Usage)
}

func (p *Printer) reportUsage(usage *Usage) {
	if usage != nil {
		p.Result("%s", usage.line())
	}
}

func (report Report) summary(files int, gaps []string) string {
	summary := fmt.Sprintf("%d new violation(s) in %d file(s), %d baselined", len(report.Violations), files, report.Baselined)
	if count := len(report.Unverified); count > 0 {
		summary += fmt.Sprintf(", %d unverified", count)
	}

	summary += "."
	if len(gaps) > 0 {
		summary += " Not fully checked: " + strings.Join(gaps, ", ") + "."
	}

	return summary
}

func violationHeader(violation Violation) string {
	return fmt.Sprintf("[VIOLATION] %s %s [Line %d]", violation.ADRID, violation.Title, violation.Line)
}

func unverifiedHeader(violation Violation) string {
	return fmt.Sprintf("[UNVERIFIED] %s %s", violation.ADRID, violation.Title)
}

func (p *Printer) reportFindings(findings []Violation, header func(Violation) string, headerStyle style) {
	byFile := map[string][]Violation{}
	for _, finding := range findings {
		byFile[finding.File] = append(byFile[finding.File], finding)
	}

	for _, file := range violatingFiles(findings) {
		fileGroup := p.Group(file)

		for _, finding := range byFile[file] {
			details := fileGroup.group(header(finding), headerStyle)
			details.write("", false, true)
			details.Field("Reasoning", "%s", finding.Reasoning)

			if finding.Code != "" {
				details.Field("Code", "%s", finding.Code)
			}

			if finding.Suggestion != "" {
				details.Field("Suggestion (unverified)", "%s", finding.Suggestion)
			}

			details.Flush()
		}

		fileGroup.Flush()
	}
}
