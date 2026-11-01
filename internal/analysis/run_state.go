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
	failedFiles   int
	violations    int
	baselined     int
	skipped       []output.FileGap
	partial       []output.FileGap
	failedChecks  []output.FailedCheck
	recorded      []output.RecordedEntry
	unrecorded    []output.UnrecordedViolation
	entries       []baseline.Entry
	collected     []Violation
	unverified    []Violation
	stageFailures []StageFailure
	coverage      output.Coverage
	projection    Projection
	uncounted     bool
	keyRejection  error
}

type fileResult struct {
	violations     int
	baselined      int
	checks         int
	judged         bool
	withoutADR     bool
	belowThreshold bool
	screenedOutBy  string
	failedChecks   []output.FailedCheck
	recorded       []output.RecordedEntry
	unrecorded     []output.UnrecordedViolation
	entries        []baseline.Entry
	records        []Violation
	unverified     []Violation
	projection     Projection
	uncounted      bool
}

func newRunState(out *output.Printer, stages []stage.Stage) *runState {
	return &runState{out: out, stages: stages, telemetry: stage.NewTelemetry(stages)}
}

func (r *runState) addSkipped(file, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.skipped = append(r.skipped, output.FileGap{File: file, Reason: reason})
}

func (r *runState) addFailedFile(file, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failedFiles++
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

// rejectKey keeps the first rejected-key error; files already in flight may report it again.
func (r *runState) rejectKey(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.keyRejection == nil {
		r.keyRejection = err
	}
}

func (r *runState) rejectedKey() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.keyRejection
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
	r.coverage.AddScreenedOut(result.screenedOutBy)
	r.failedChecks = append(r.failedChecks, result.failedChecks...)
	r.recorded = append(r.recorded, result.recorded...)
	r.unrecorded = append(r.unrecorded, result.unrecorded...)
	r.entries = append(r.entries, result.entries...)
	r.collected = append(r.collected, result.records...)
	r.unverified = append(r.unverified, result.unverified...)
	r.projection.Calls += result.projection.Calls
	r.projection.Cached += result.projection.Cached
	r.projection.Usage.InputTokens += result.projection.Usage.InputTokens
	r.uncounted = r.uncounted || result.uncounted
}

func (r *runState) projected() Projection {
	projection := r.projection
	projection.Usage.Reported = !r.uncounted

	return projection
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// A file skipped as too large in CI mode is not an error, so it alone never makes a run count as unchecked.
func (r *runState) checkedNothing() bool {
	return (r.failedFiles > 0 || len(r.failedChecks) > 0) && r.coverage.ADRChecks == 0
}
