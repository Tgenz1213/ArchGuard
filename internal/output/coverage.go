package output

import (
	"fmt"
	"slices"
	"strings"
)

// Coverage is how much of a run reached the model. Skipped and partly checked files are in Gaps, not here.
type Coverage struct {
	FilesJudged         int `json:"files_judged"`
	ADRChecks           int `json:"adr_checks"`
	FilesWithoutADR     int `json:"files_without_relevant_adr"`
	FilesBelowThreshold int `json:"files_below_threshold"`
	FilesScreenedOut    int `json:"files_screened_out"`
	// The nodes that dropped those files' every candidate, sorted so a report doesn't change with the order files finish.
	ScreenedOutBy []string `json:"-"`
}

// AddScreenedOut counts a file whose every candidate the named node dropped; an empty name counts nothing.
func (c *Coverage) AddScreenedOut(node string) {
	if node == "" {
		return
	}

	c.FilesScreenedOut++
	if !slices.Contains(c.ScreenedOutBy, node) {
		c.ScreenedOutBy = append(c.ScreenedOutBy, node)
		slices.Sort(c.ScreenedOutBy)
	}
}

func (c Coverage) screeningNodes() string {
	if len(c.ScreenedOutBy) == 1 {
		return "the " + c.ScreenedOutBy[0] + " node"
	}

	return "a node (" + strings.Join(c.ScreenedOutBy, ", ") + ")"
}

func (c Coverage) line() string {
	line := fmt.Sprintf("Checked %d file(s) against %d ADR check(s); %d file(s) had no relevant ADR", c.FilesJudged, c.ADRChecks, c.FilesWithoutADR)
	if c.FilesBelowThreshold > 0 {
		line += fmt.Sprintf(", %d of them because every candidate scored below the threshold", c.FilesBelowThreshold)
	}

	if c.FilesScreenedOut > 0 {
		lead := "%d because"
		if c.FilesBelowThreshold == 0 {
			lead = "%d of them because"
		}

		line += fmt.Sprintf(", "+lead+" %s dropped every candidate", c.FilesScreenedOut, c.screeningNodes())
	}

	return line + "."
}

func (c Coverage) cleanResult(baselined int) string {
	switch {
	case c.FilesJudged == 0 && c.FilesBelowThreshold > 0:
		return "No new architectural violations found, but no file was checked against an ADR: every candidate scored below the threshold. The right threshold depends on the embedding model; run with --debug to see the scores."
	case c.FilesJudged == 0 && c.FilesScreenedOut > 0:
		return "No new architectural violations found, but no file was checked against an ADR: " + c.screeningNodes() + " dropped every candidate. Run with --debug to see the scores."
	case c.FilesJudged == 0:
		return "No new architectural violations found, but no file was checked against an ADR."
	case baselined > 0:
		return fmt.Sprintf("No new architectural violations found (%d baselined).", baselined)
	default:
		return "No new architectural violations found."
	}
}
