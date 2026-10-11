package analysis

import (
	"sync"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/output"
)

// runState holds what a run collects across files; the fields after mu are guarded by it.
type runState struct {
	out       *output.Printer
	stages    []stage.Stage
	telemetry *stage.Telemetry

	mu            sync.Mutex
	violations    int
	baselined     int
	skipped       []output.FileGap
	partial       []output.FileGap
	failedChecks  []output.FailedCheck
	recorded      []output.RecordedEntry
	unrecorded    []output.UnrecordedViolation
	entries       []baseline.Entry
	collected     []Violation
	stageFailures []StageFailure
	coverage      output.Coverage
}

type fileResult struct {
	violations     int
	baselined      int
	checks         int
	judged         bool
	withoutADR     bool
	belowThreshold bool
	failedChecks   []output.FailedCheck
	recorded       []output.RecordedEntry
	unrecorded     []output.UnrecordedViolation
	entries        []baseline.Entry
	records        []Violation
}

func newRunState(out *output.Printer, stages []stage.Stage) *runState {
	return &runState{out: out, stages: stages, telemetry: stage.NewTelemetry(stages)}
}

func (r *runState) addSkipped(file, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.skipped = append(r.skipped, output.FileGap{File: file, Reason: reason})
}

func (r *runState) addPartial(file, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.partial = append(r.partial, output.FileGap{File: file, Reason: reason})
}

func (r *runState) addStageFailure(failure StageFailure) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stageFailures = append(r.stageFailures, failure)
}

func (r *runState) merge(result *fileResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.violations += result.violations
	r.baselined += result.baselined
	r.coverage.ADRChecks += result.checks
	r.coverage.FilesJudged += boolToInt(result.judged)
	r.coverage.FilesWithoutADR += boolToInt(result.withoutADR)
	r.coverage.FilesBelowThreshold += boolToInt(result.belowThreshold)
	r.failedChecks = append(r.failedChecks, result.failedChecks...)
	r.recorded = append(r.recorded, result.recorded...)
	r.unrecorded = append(r.unrecorded, result.unrecorded...)
	r.entries = append(r.entries, result.entries...)
	r.collected = append(r.collected, result.records...)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
