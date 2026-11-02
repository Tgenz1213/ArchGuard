package output

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
)

type FileGap struct {
	File   string
	Reason string
}

type ADRGap struct {
	ADR    string
	Reason string
}

type FailedCheck struct {
	File   string
	ADRID  string
	Title  string
	Reason string
}

type FailedNode struct {
	Node   string
	File   string
	Reason string
}

// Gaps is everything a run could not check in full.
type Gaps struct {
	SkippedFiles  []FileGap
	FailedChecks  []FailedCheck
	FailedNodes   []FailedNode
	PartialFiles  []FileGap
	UnindexedADRs []ADRGap
}

func (g Gaps) sortedGaps() Gaps {
	g.SkippedFiles = append([]FileGap(nil), g.SkippedFiles...)
	g.FailedChecks = sortedByFileAndADR(g.FailedChecks, func(c FailedCheck) (string, string) {
		return c.File, c.ADRID
	})
	g.FailedNodes = append([]FailedNode(nil), g.FailedNodes...)
	g.PartialFiles = append([]FileGap(nil), g.PartialFiles...)
	g.UnindexedADRs = append([]ADRGap(nil), g.UnindexedADRs...)

	sort.SliceStable(g.SkippedFiles, func(i, j int) bool { return g.SkippedFiles[i].File < g.SkippedFiles[j].File })
	sort.SliceStable(g.PartialFiles, func(i, j int) bool { return g.PartialFiles[i].File < g.PartialFiles[j].File })
	sort.SliceStable(g.UnindexedADRs, func(i, j int) bool { return g.UnindexedADRs[i].ADR < g.UnindexedADRs[j].ADR })
	sort.SliceStable(g.FailedNodes, func(i, j int) bool {
		left, right := g.FailedNodes[i], g.FailedNodes[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.Node < right.Node
	})

	return g
}

func (g Gaps) gapSummary() []string {
	var parts []string

	if count := len(g.SkippedFiles); count > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) skipped", count))
	}

	if count := len(g.PartialFiles); count > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) partly checked", count))
	}

	if count := len(g.FailedChecks); count > 0 {
		parts = append(parts, fmt.Sprintf("%d ADR check(s) failed", count))
	}

	if count := len(g.FailedNodes); count > 0 {
		parts = append(parts, fmt.Sprintf("%d node failure(s)", count))
	}

	if count := len(g.UnindexedADRs); count > 0 {
		parts = append(parts, fmt.Sprintf("%d ADR(s) not indexed", count))
	}

	return parts
}

func (p *Printer) reportGaps(g Gaps) {
	p.section("Skipped files:", formatEach(g.SkippedFiles, fileGapLine))
	p.section("Partly checked files:", formatEach(g.PartialFiles, fileGapLine))
	p.section("Failed ADR checks:", formatEach(g.FailedChecks, func(c FailedCheck) string {
		return fmt.Sprintf("%s: ADR %s %s: %s", c.File, c.ADRID, c.Title, c.Reason)
	}))
	p.section("Failed nodes:", formatEach(g.FailedNodes, func(f FailedNode) string {
		return fmt.Sprintf("%s: node %s: %s", f.File, f.Node, f.Reason)
	}))
	p.section(
		"ADRs not in the index (no file was checked against them):",
		formatEach(g.UnindexedADRs, func(a ADRGap) string { return fmt.Sprintf("%s: %s", a.ADR, a.Reason) }),
	)
}

func (p *Printer) section(heading string, lines []string) {
	if len(lines) == 0 {
		return
	}

	p.Result("%s", heading)

	list := p.Indented()
	for _, line := range lines {
		list.Result("%s", line)
	}

	p.Result("")
}

func fileGapLine(gap FileGap) string { return fmt.Sprintf("%s: %s", gap.File, gap.Reason) }

func formatEach[T any](items []T, format func(T) string) []string {
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = format(item)
	}

	return lines
}

func sortedByFileAndADR[T any](items []T, key func(T) (file, adrID string)) []T {
	sorted := slices.Clone(items)
	slices.SortStableFunc(sorted, func(a, b T) int {
		aFile, aADR := key(a)
		bFile, bADR := key(b)

		return cmp.Or(cmp.Compare(aFile, bFile), cmp.Compare(aADR, bADR))
	})

	return sorted
}
