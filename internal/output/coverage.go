package output

import "fmt"

// Coverage is how much of a run reached the model. Skipped and partly checked files are in Gaps, not here.
type Coverage struct {
	FilesJudged     int `json:"files_judged"`
	ADRChecks       int `json:"adr_checks"`
	FilesWithoutADR int `json:"files_without_relevant_adr"`
	// A subset of FilesWithoutADR: files with candidate ADRs that all scored below the threshold.
	FilesBelowThreshold int `json:"files_below_threshold"`
}

func (c Coverage) line() string {
	line := fmt.Sprintf("Checked %d file(s) against %d ADR check(s); %d file(s) had no relevant ADR", c.FilesJudged, c.ADRChecks, c.FilesWithoutADR)
	if c.FilesBelowThreshold > 0 {
		line += fmt.Sprintf(", %d of them because every candidate scored below the threshold", c.FilesBelowThreshold)
	}

	return line + "."
}

func (c Coverage) cleanResult(baselined int) string {
	switch {
	case c.FilesJudged == 0 && c.FilesBelowThreshold > 0:
		return "No new architectural violations found, but no file was checked against an ADR: every candidate scored below the threshold. The right threshold depends on the embedding model; run with --debug to see the scores."
	case c.FilesJudged == 0:
		return "No new architectural violations found, but no file was checked against an ADR."
	case baselined > 0:
		return fmt.Sprintf("No new architectural violations found (%d baselined).", baselined)
	default:
		return "No new architectural violations found."
	}
}
