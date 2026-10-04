package analysis

import (
	"fmt"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/output"
)

func (e *Engine) Report() output.Report {
	report := output.Report{Gaps: e.gaps(), Baselined: e.Baselined}

	for _, violation := range e.CollectedViolations {
		report.Violations = append(report.Violations, output.Violation{
			File:       violation.File,
			Line:       violation.Line,
			Verified:   !violation.unverified,
			ADRID:      violation.ADRID,
			Title:      violation.ADRTitle,
			Reasoning:  violation.Reasoning,
			Code:       violation.QuotedCode,
			Suggestion: violation.Suggestion,
		})
	}

	return report
}

func (e *Engine) BaselineReport(path string) output.BaselineReport {
	return output.BaselineReport{
		Path:       path,
		Recorded:   e.RecordedEntries,
		Unrecorded: e.UnrecordedViolations,
		Gaps:       e.gaps(),
	}
}

func (e *Engine) gaps() output.Gaps {
	gaps := output.Gaps{SkippedFiles: e.SkippedFiles, FailedChecks: e.FailedChecks, PartialFiles: e.PartialFiles}

	for _, failure := range e.StageFailures {
		gaps.FailedStages = append(gaps.FailedStages, output.FailedStage{Stage: failure.Stage, File: failure.File, Reason: failure.Error})
	}

	return gaps
}

func violationLine(violation Violation, baselined bool) string {
	line := fmt.Sprintf("%s: violates ADR %s (%s)", violation.File, violation.ADRID, violation.ADRTitle)

	if violation.unverified {
		line += ", quoted code not found"
	} else {
		line += fmt.Sprintf(", line %d", violation.Line)
	}

	if baselined {
		line += " [baselined]"
	}

	return line
}

func checkOutcomeLine(hit stage.Candidate, outcome string, cached bool) string {
	line := fmt.Sprintf("ADR %s (%s), similarity %.2f: %s", hit.ADR.ID, hit.ADR.Title, hit.Score, outcome)
	if cached {
		line += " (cached)"
	}

	return line
}
