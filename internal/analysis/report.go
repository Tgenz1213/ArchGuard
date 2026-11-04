package analysis

import (
	"fmt"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/output"
)

func (e *Engine) Report() output.Report {
	return output.Report{
		Violations: toOutputFindings(e.CollectedViolations),
		Unverified: toOutputFindings(e.UnverifiedFindings),
		Gaps:       e.gaps(),
		Baselined:  e.Baselined,
		Coverage:   e.Coverage,
		DryRun:     e.DryRunSummary(),
	}
}

func toOutputFindings(violations []Violation) []output.Violation {
	var found []output.Violation

	for _, violation := range violations {
		found = append(found, output.Violation{
			File:       violation.File,
			Line:       violation.Line,
			ADRID:      violation.ADRID,
			Title:      violation.ADRTitle,
			Reasoning:  violation.Reasoning,
			Code:       violation.QuotedCode,
			Suggestion: violation.Suggestion,
		})
	}

	return found
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
	gaps := output.Gaps{SkippedFiles: e.SkippedFiles, FailedChecks: e.FailedChecks, PartialFiles: e.PartialFiles, UnindexedADRs: e.UnindexedADRs}

	for _, failure := range e.NodeFailures {
		gaps.FailedNodes = append(gaps.FailedNodes, output.FailedNode{Node: failure.Node, File: failure.File, Reason: failure.Error})
	}

	return gaps
}

func violationLine(violation Violation, baselined bool) string {
	line := fmt.Sprintf("%s: violates ADR %s (%s), line %d", violation.File, violation.ADRID, violation.ADRTitle, violation.Line)
	if violation.unverified {
		line = fmt.Sprintf("%s: unverified finding for ADR %s (%s), quoted code not found", violation.File, violation.ADRID, violation.ADRTitle)
	}

	if baselined {
		line += " [baselined]"
	}

	return line
}

func checkOutcomeLine(hit node.Candidate, outcome string, cached bool) string {
	line := fmt.Sprintf("ADR %s (%s), score %.2f: %s", hit.ADR.ID, hit.ADR.Title, hit.Score, outcome)
	if cached {
		line += " (cached)"
	}

	return line
}
