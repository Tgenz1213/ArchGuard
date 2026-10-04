package analysis

import (
	"fmt"

	"github.com/tgenz1213/archguard/internal/output"
)

func (e *Engine) Report() output.Report {
	report := output.Report{
		SkippedFiles: e.SkippedFiles,
		FailedChecks: e.FailedChecks,
		Baselined:    e.Baselined,
	}

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

	for _, failure := range e.StageFailures {
		report.FailedStages = append(report.FailedStages, output.FailedStage{Stage: failure.Stage, File: failure.File, Reason: failure.Error})
	}

	return report
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
