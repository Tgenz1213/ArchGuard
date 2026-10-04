package output

import (
	"fmt"
	"sort"
)

type FailedStage struct {
	Stage  string
	File   string
	Reason string
}

// Gaps is everything a run could not check in full.
type Gaps struct {
	SkippedFiles []SkippedFile
	FailedChecks []FailedCheck
	FailedStages []FailedStage
	PartialFiles []SkippedFile
}

func (gaps Gaps) sortedGaps() Gaps {
	gaps.SkippedFiles = append([]SkippedFile(nil), gaps.SkippedFiles...)
	gaps.FailedChecks = append([]FailedCheck(nil), gaps.FailedChecks...)
	gaps.FailedStages = append([]FailedStage(nil), gaps.FailedStages...)
	gaps.PartialFiles = append([]SkippedFile(nil), gaps.PartialFiles...)

	sort.SliceStable(gaps.SkippedFiles, func(i, j int) bool { return gaps.SkippedFiles[i].File < gaps.SkippedFiles[j].File })
	sort.SliceStable(gaps.PartialFiles, func(i, j int) bool { return gaps.PartialFiles[i].File < gaps.PartialFiles[j].File })
	sort.SliceStable(gaps.FailedChecks, func(i, j int) bool {
		left, right := gaps.FailedChecks[i], gaps.FailedChecks[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.ADRID < right.ADRID
	})
	sort.SliceStable(gaps.FailedStages, func(i, j int) bool {
		left, right := gaps.FailedStages[i], gaps.FailedStages[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.Stage < right.Stage
	})

	return gaps
}

func (gaps Gaps) gapSummary() []string {
	var parts []string

	if count := len(gaps.SkippedFiles); count > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) skipped", count))
	}

	if count := len(gaps.PartialFiles); count > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) partly checked", count))
	}

	if count := len(gaps.FailedChecks); count > 0 {
		parts = append(parts, fmt.Sprintf("%d ADR check(s) failed", count))
	}

	if count := len(gaps.FailedStages); count > 0 {
		parts = append(parts, fmt.Sprintf("%d stage failure(s)", count))
	}

	return parts
}

func (p *Printer) reportGaps(gaps Gaps) {
	if len(gaps.SkippedFiles) > 0 {
		p.Result("Skipped files:")

		list := p.Indented()
		for _, skipped := range gaps.SkippedFiles {
			list.Result("%s: %s", skipped.File, skipped.Reason)
		}

		p.Result("")
	}

	if len(gaps.PartialFiles) > 0 {
		p.Result("Partly checked files:")

		list := p.Indented()
		for _, partial := range gaps.PartialFiles {
			list.Result("%s: %s", partial.File, partial.Reason)
		}

		p.Result("")
	}

	if len(gaps.FailedChecks) > 0 {
		p.Result("Failed ADR checks:")

		list := p.Indented()
		for _, check := range gaps.FailedChecks {
			list.Result("%s: ADR %s %s: %s", check.File, check.ADRID, check.Title, check.Reason)
		}

		p.Result("")
	}

	if len(gaps.FailedStages) > 0 {
		p.Result("Failed stages:")

		list := p.Indented()
		for _, failure := range gaps.FailedStages {
			list.Result("%s: stage %s: %s", failure.File, failure.Stage, failure.Reason)
		}

		p.Result("")
	}
}
