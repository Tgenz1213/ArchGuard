package output

import (
	"fmt"
	"sort"
	"strings"
)

type FailedStage struct {
	Stage  string
	File   string
	Reason string
}

type Report struct {
	Violations   []Violation
	SkippedFiles []SkippedFile
	FailedChecks []FailedCheck
	FailedStages []FailedStage
	Baselined    int
}

func (report Report) sorted() Report {
	report.Violations = append([]Violation(nil), report.Violations...)
	report.SkippedFiles = append([]SkippedFile(nil), report.SkippedFiles...)
	report.FailedChecks = append([]FailedCheck(nil), report.FailedChecks...)
	report.FailedStages = append([]FailedStage(nil), report.FailedStages...)

	sort.SliceStable(report.Violations, func(i, j int) bool {
		left, right := report.Violations[i], report.Violations[j]
		if left.File != right.File {
			return left.File < right.File
		}

		if left.Line != right.Line {
			return left.Line < right.Line
		}

		return left.ADRID < right.ADRID
	})
	sort.SliceStable(report.SkippedFiles, func(i, j int) bool { return report.SkippedFiles[i].File < report.SkippedFiles[j].File })
	sort.SliceStable(report.FailedChecks, func(i, j int) bool {
		left, right := report.FailedChecks[i], report.FailedChecks[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.ADRID < right.ADRID
	})
	sort.SliceStable(report.FailedStages, func(i, j int) bool {
		left, right := report.FailedStages[i], report.FailedStages[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.Stage < right.Stage
	})

	return report
}

func (report Report) gaps() []string {
	var parts []string

	if count := len(report.SkippedFiles); count > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) skipped", count))
	}

	if count := len(report.FailedChecks); count > 0 {
		parts = append(parts, fmt.Sprintf("%d ADR check(s) failed", count))
	}

	if count := len(report.FailedStages); count > 0 {
		parts = append(parts, fmt.Sprintf("%d stage failure(s)", count))
	}

	return parts
}

func (report Report) violatingFiles() []string {
	seen := map[string]struct{}{}

	var files []string

	for _, violation := range report.Violations {
		if _, ok := seen[violation.File]; !ok {
			seen[violation.File] = struct{}{}
			files = append(files, violation.File)
		}
	}

	sort.Strings(files)

	return files
}

func (p *Printer) Report(report Report) {
	report = report.sorted()
	files := report.violatingFiles()
	gaps := report.gaps()

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

	if len(report.SkippedFiles) > 0 {
		p.Result("Skipped files:")

		list := p.Indented()
		for _, skipped := range report.SkippedFiles {
			list.Result("%s: %s", skipped.File, skipped.Reason)
		}

		p.Result("")
	}

	if len(report.FailedChecks) > 0 {
		p.Result("Failed ADR checks:")

		list := p.Indented()
		for _, check := range report.FailedChecks {
			list.Result("%s: ADR %s %s: %s", check.File, check.ADRID, check.Title, check.Reason)
		}

		p.Result("")
	}

	if len(report.FailedStages) > 0 {
		p.Result("Failed stages:")

		list := p.Indented()
		for _, failure := range report.FailedStages {
			list.Result("%s: stage %s: %s", failure.File, failure.Stage, failure.Reason)
		}

		p.Result("")
	}

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
