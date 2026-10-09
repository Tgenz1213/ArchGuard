package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
)

type checkRun struct {
	setup        runSetup
	opts         checkCmd
	log          *output.Printer
	report       *output.Printer
	jsonDest     io.Writer
	reportBuffer bytes.Buffer
}

func runCheck(ctx context.Context, setup runSetup, opts checkCmd, colors streamColors) (code ExitCode, err error) {
	run, err := newCheckRun(setup, opts, colors)
	if err != nil {
		return ExitError, err
	}

	defer func() {
		if werr := run.report.Err(); werr != nil && !opts.jsonOutput() && code != ExitInterrupted {
			code, err = ExitError, errors.Join(outputWriteError(werr), err)
		}
	}()

	if opts.Format == "json" && opts.UpdateBaseline {
		run.log.Note("--format json has no effect with --update-baseline; ignoring it.")
	}

	store, err := index.NewVectorStore(setup.cfg, run.log)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	if err := run.loadIndex(ctx, store); err != nil {
		return ExitIndexError, err
	}

	run.noteIgnoredBaselineFlags()

	contentProvider := opts.contentProvider(run.log)

	run.log.Debug("Mode Enabled")

	loadedBaseline, err := run.loadBaseline()
	if err != nil {
		return ExitError, err
	}

	engine := run.newEngine(store, contentProvider, loadedBaseline)

	runErr := engine.Run(ctx)
	if runErr != nil && ctx.Err() != nil {
		return ExitInterrupted, errInterrupted
	}

	if opts.UpdateBaseline {
		return run.finishUpdateBaseline(engine, runErr)
	}

	return run.finishCheck(engine, runErr)
}

func newCheckRun(setup runSetup, opts checkCmd, colors streamColors) (*checkRun, error) {
	run := &checkRun{setup: setup, opts: opts, log: colors.stderrPrinter(opts.Debug)}

	if opts.Output == "" {
		run.report = colors.stdoutPrinter(false)
		run.jsonDest = os.Stdout

		return run, nil
	}

	if err := checkReportDestination(opts.Output); err != nil {
		return nil, err
	}

	run.report = output.New(&run.reportBuffer, false)
	run.jsonDest = &run.reportBuffer

	return run, nil
}

func (r *checkRun) noteIgnoredBaselineFlags() {
	if r.opts.UpdateBaseline && (len(r.opts.Paths) > 0 || r.opts.Staged) {
		r.log.Note("--update-baseline always scans the full repository; ignoring --staged and any file arguments.")
	}

	if r.opts.BaselineReason != "" && !r.opts.UpdateBaseline {
		r.log.Note("--baseline-reason has no effect without --update-baseline; ignoring it.")
	}
}

func (r *checkRun) loadBaseline() (*baseline.Baseline, error) {
	loaded, err := baseline.Load(baseline.Path)
	if err == nil {
		return loaded, nil
	}

	if !r.opts.UpdateBaseline {
		return nil, fmt.Errorf("failed to load baseline file %s: %w (fix it, or regenerate it with `archguard check --update-baseline`)", baseline.Path, err)
	}

	r.log.Warn("failed to load existing baseline file %s (baseline reasons will not carry forward): %v", baseline.Path, err)

	return loaded, nil
}

func (r *checkRun) newEngine(store index.VectorStore, contentProvider analysis.ContentProvider, loaded *baseline.Baseline) *analysis.Engine {
	setup, opts := r.setup, r.opts

	engine := analysis.NewEngine(setup.cfg, store, setup.chat, setup.embed, contentProvider)
	engine.Debug = opts.Debug
	engine.CI = opts.CI

	analysisCache, err := cache.NewCache(".")
	if err != nil {
		r.log.Warn("analysis cache disabled: %v", err)
	}

	engine.Cache = analysisCache

	engine.Stages = analysis.BuildStages(setup.cfg, store, setup.embed, r.log)
	engine.Baseline = loaded
	engine.UpdateBaseline = opts.UpdateBaseline
	engine.BaselineReason = opts.BaselineReason
	engine.JSONOutput = opts.jsonOutput()
	engine.Out = r.log
	engine.SuggestFixes = opts.SuggestFixes

	return engine
}

func (r *checkRun) finishUpdateBaseline(engine *analysis.Engine, runErr error) (ExitCode, error) {
	if runErr != nil {
		return exitCodeForAnalysisError(runErr), fmt.Errorf("analysis failed: %w", runErr)
	}

	if stageCode, stageErr := stageFailureExit(engine.StageFailures); stageErr != nil {
		return stageCode, fmt.Errorf("%w; baseline not written", stageErr)
	}

	if err := engine.CollectedBaseline.Save(baseline.Path); err != nil {
		return ExitError, fmt.Errorf("failed to write baseline file %s: %w", baseline.Path, err)
	}

	r.report.BaselineReport(engine.BaselineReport(baseline.Path))

	if err := r.saveReportFile(); err != nil {
		return ExitError, err
	}

	return ExitSuccess, nil
}

func (r *checkRun) finishCheck(engine *analysis.Engine, runErr error) (ExitCode, error) {
	stageCode, stageErr := stageFailureExit(engine.StageFailures)

	if !r.opts.jsonOutput() && output.InGitHubActions() {
		for _, v := range engine.CollectedViolations {
			r.log.Annotation(v.Annotation())
		}
	}

	var analysisErr error
	if runErr != nil {
		analysisErr = fmt.Errorf("analysis failed: %w", runErr)
	}

	if err := r.writeReport(engine, runErr); err != nil {
		return ExitError, errors.Join(err, stageErr, analysisErr)
	}

	if stageErr != nil {
		return stageCode, stageErr
	}

	if isDriftError(runErr) {
		return ExitDriftDetected, nil
	}

	if runErr != nil {
		return ExitError, analysisErr
	}

	return ExitSuccess, nil
}

// A run whose analysis failed outright in text mode has no report to write.
func (r *checkRun) writeReport(engine *analysis.Engine, runErr error) error {
	analysisFailed := runErr != nil && !isDriftError(runErr)

	if r.opts.jsonOutput() {
		if err := writeCheckReport(r.jsonDest, engine.CollectedViolations, engine.CollectedStages, engine.StageFailures); err != nil {
			return fmt.Errorf("failed to write json report: %w", err)
		}

		if len(engine.SkippedFiles) > 0 || len(engine.FailedChecks) > 0 {
			r.log.Warn("%d file(s) skipped and %d ADR check(s) failed; compliance was not fully verified.", len(engine.SkippedFiles), len(engine.FailedChecks))
		}
	} else if !analysisFailed {
		r.report.Report(engine.Report())
	}

	if r.opts.jsonOutput() || !analysisFailed {
		return r.saveReportFile()
	}

	return nil
}

func (r *checkRun) saveReportFile() error {
	if r.opts.Output == "" {
		return nil
	}

	if err := saveReport(r.opts.Output, r.reportBuffer.Bytes()); err != nil {
		return err
	}

	r.log.Info("Report written to %s", r.opts.Output)

	return nil
}

type checkReport struct {
	Violations []analysis.Violation    `json:"violations"`
	Count      int                     `json:"count"`
	Stages     []stage.Stats           `json:"stages"`
	Failures   []analysis.StageFailure `json:"failures,omitempty"`
}

func writeCheckReport(w io.Writer, violations []analysis.Violation, stages []stage.Stats, failures []analysis.StageFailure) error {
	if violations == nil {
		violations = []analysis.Violation{}
	}

	if stages == nil {
		stages = []stage.Stats{}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(checkReport{Violations: violations, Count: len(violations), Stages: stages, Failures: failures})
}

// A precondition failure outranks an unavailable dependency when a run has both.
func stageFailureExit(failures []analysis.StageFailure) (ExitCode, error) {
	if len(failures) == 0 {
		return ExitSuccess, nil
	}

	code := ExitStageUnavailable
	for _, f := range failures {
		if f.Kind == stage.KindPreconditionNotMet {
			code = ExitStagePrecondition
		}
	}

	return code, fmt.Errorf("%d stage failure(s) with on_error: fail; compliance was not verified", len(failures))
}

func (c checkCmd) contentProvider(out *output.Printer) analysis.ContentProvider {
	switch {
	case c.UpdateBaseline:
		return &analysis.AllProvider{}
	case slices.Contains(c.Paths, "."):
		if extras := slices.DeleteFunc(slices.Clone(c.Paths), func(path string) bool { return path == "." }); len(extras) > 0 {
			out.Note("\".\" scans the whole repository; ignoring extra path argument(s): %v", extras)
		}

		return &analysis.AllProvider{}
	case len(c.Paths) > 0:
		return &analysis.MultiFileProvider{Paths: c.Paths}
	case c.Staged:
		return &analysis.StagedProvider{}
	case c.All:
		return &analysis.AllProvider{}
	default:
		return &analysis.UncommittedProvider{}
	}
}

func isDriftError(err error) bool {
	var driftErr *analysis.DriftDetectedError

	return errors.As(err, &driftErr)
}

func exitCodeForAnalysisError(err error) ExitCode {
	if isDriftError(err) {
		return ExitDriftDetected
	}

	return ExitError
}
