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

func runCheck(ctx context.Context, setup runSetup, opts checkCmd, colors streamColors) (code ExitCode, err error) {
	cfg := setup.cfg
	files := opts.Paths

	jsonOutput := opts.jsonOutput()
	logPrinter := colors.stderrPrinter(opts.Debug)

	var (
		reportBuffer  bytes.Buffer
		reportPrinter *output.Printer
		jsonDest      io.Writer
	)

	if opts.Output != "" {
		if err := checkReportDestination(opts.Output); err != nil {
			return ExitError, err
		}

		reportPrinter = output.New(&reportBuffer, false)
		jsonDest = &reportBuffer
	} else {
		reportPrinter = colors.stdoutPrinter(false)
		jsonDest = os.Stdout
	}

	saveReportFile := func() error {
		if opts.Output == "" {
			return nil
		}

		if err := saveReport(opts.Output, reportBuffer.Bytes()); err != nil {
			return err
		}

		logPrinter.Info("Report written to %s", opts.Output)

		return nil
	}

	defer func() {
		if werr := reportPrinter.Err(); werr != nil && !jsonOutput && code != ExitInterrupted {
			code, err = ExitError, errors.Join(outputWriteError(werr), err)
		}
	}()

	if opts.Format == "json" && opts.UpdateBaseline {
		logPrinter.Note("--format json has no effect with --update-baseline; ignoring it.")
	}

	store, err := index.NewVectorStore(cfg, logPrinter)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %v", err)
	}

	// Held until we know whether a rebuild will fetch the ADRs again and repeat these warnings.
	fetchOut := logPrinter.Group("")

	localProvider := index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses)
	localProvider.SetIDPattern(setup.adrIDPattern)
	localProvider.SetFrontmatterMappings(setup.frontmatterMappings)
	localProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
	localProvider.SetPrinter(fetchOut)
	var providers []index.Provider
	providers = append(providers, localProvider)

	if cfg.Analysis.Confluence.Enabled {
		confluenceProvider := index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		)
		confluenceProvider.SetFrontmatterMappings(setup.frontmatterMappings)
		confluenceProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
		confluenceProvider.SetPrinter(fetchOut)
		providers = append(providers, confluenceProvider)
	}

	adrProvider := index.NewCompositeProvider(providers...)
	adrProvider.SetPrinter(fetchOut)

	validADRs, _, err := adrProvider.GetADRs(ctx)
	if err != nil {
		fetchOut.Flush()
		return ExitIndexError, fmt.Errorf("failed to fetch ADRs: %v", err)
	}

	currentHash, err := store.CalculateHash(validADRs, cfg.VectorStore.Model)
	if err != nil {
		fetchOut.Flush()
		return ExitIndexError, fmt.Errorf("failed to calculate index hash: %v", err)
	}

	if err := store.Load(setup.indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err == nil {
		fetchOut.Flush()
	} else {
		logPrinter.Info("Index metadata mismatch or missing index. Triggering index rebuild: %v", err)

		if _, err := runIndex(ctx, setup, logPrinter); err != nil {
			return ExitIndexError, fmt.Errorf("index rebuild failed: %v", err)
		}

		currentHash, err = store.CalculateHash(validADRs, cfg.VectorStore.Model)
		if err != nil {
			return ExitIndexError, fmt.Errorf("failed to calculate rebuilt index hash: %v", err)
		}

		if err := store.Load(setup.indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
			return ExitIndexError, fmt.Errorf("failed to load rebuilt index: %v", err)
		}
	}

	if opts.UpdateBaseline && (len(files) > 0 || opts.Staged) {
		logPrinter.Note("--update-baseline always scans the full repository; ignoring --staged and any file arguments.")
	}

	if opts.BaselineReason != "" && !opts.UpdateBaseline {
		logPrinter.Note("--baseline-reason has no effect without --update-baseline; ignoring it.")
	}

	contentProvider := resolveContentProvider(logPrinter, files, opts.Staged, opts.All, opts.UpdateBaseline)

	logPrinter.Debug("Mode Enabled")

	var loadedBaseline *baseline.Baseline

	loadedBaseline, err = baseline.Load(baseline.Path)
	if err != nil {
		if !opts.UpdateBaseline {
			return ExitError, fmt.Errorf("failed to load baseline file %s: %v (fix it, or regenerate it with `archguard check --update-baseline`)", baseline.Path, err)
		}

		logPrinter.Warn("failed to load existing baseline file %s (baseline reasons will not carry forward): %v", baseline.Path, err)
	}

	engine := analysis.NewEngine(cfg, store, setup.chat, setup.embed, contentProvider)
	engine.Debug = opts.Debug
	engine.CI = opts.CI

	analysisCache, cacheErr := cache.NewCache(".")
	if cacheErr != nil {
		logPrinter.Warn("analysis cache disabled: %v", cacheErr)
	}

	engine.Cache = analysisCache

	engine.Stages = analysis.BuildStages(cfg, store, setup.embed, logPrinter)
	engine.Baseline = loadedBaseline
	engine.UpdateBaseline = opts.UpdateBaseline
	engine.BaselineReason = opts.BaselineReason
	engine.JSONOutput = jsonOutput
	engine.Out = logPrinter
	engine.SuggestFixes = opts.SuggestFixes

	runErr := engine.Run(ctx)
	if runErr != nil && ctx.Err() != nil {
		return ExitInterrupted, errInterrupted
	}

	stageFailureCode, stageFailureErr := stageFailureExit(engine.StageFailures)

	if opts.UpdateBaseline {
		if runErr != nil {
			return exitCodeForAnalysisError(runErr), fmt.Errorf("analysis failed: %v", runErr)
		}

		if stageFailureErr != nil {
			return stageFailureCode, fmt.Errorf("%v; baseline not written", stageFailureErr)
		}

		if err := engine.CollectedBaseline.Save(baseline.Path); err != nil {
			return ExitError, fmt.Errorf("failed to write baseline file %s: %v", baseline.Path, err)
		}

		reportPrinter.BaselineReport(engine.BaselineReport(baseline.Path))

		if err := saveReportFile(); err != nil {
			return ExitError, err
		}

		return ExitSuccess, nil
	}

	if !jsonOutput && output.InGitHubActions() {
		for _, v := range engine.CollectedViolations {
			logPrinter.Annotation(v.Annotation())
		}
	}

	var analysisErr error
	if runErr != nil {
		analysisErr = fmt.Errorf("analysis failed: %v", runErr)
	}

	if jsonOutput {
		if err := writeCheckReport(jsonDest, engine.CollectedViolations, engine.CollectedStages, engine.StageFailures); err != nil {
			return ExitError, errors.Join(fmt.Errorf("failed to write json report: %w", err), stageFailureErr, analysisErr)
		}
	}

	if jsonOutput && (len(engine.SkippedFiles) > 0 || len(engine.FailedChecks) > 0) {
		logPrinter.Warn("%d file(s) skipped and %d ADR check(s) failed; compliance was not fully verified.", len(engine.SkippedFiles), len(engine.FailedChecks))
	}

	var driftErr *analysis.DriftDetectedError

	analysisFailed := runErr != nil && !errors.As(runErr, &driftErr)
	if !jsonOutput && !analysisFailed {
		reportPrinter.Report(engine.Report())
	}

	if jsonOutput || !analysisFailed {
		if err := saveReportFile(); err != nil {
			return ExitError, errors.Join(err, stageFailureErr, analysisErr)
		}
	}

	if stageFailureErr != nil {
		return stageFailureCode, stageFailureErr
	}

	if errors.As(runErr, &driftErr) {
		return ExitDriftDetected, nil
	}

	if runErr != nil {
		return exitCodeForAnalysisError(runErr), analysisErr
	}

	return ExitSuccess, nil
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

func resolveContentProvider(out *output.Printer, files []string, staged, all, updateBaseline bool) analysis.ContentProvider {
	if updateBaseline {
		return &analysis.AllProvider{}
	}

	if len(files) > 0 {
		if slices.Contains(files, ".") {
			var extras []string
			for _, f := range files {
				if f != "." {
					extras = append(extras, f)
				}
			}

			if len(extras) > 0 {
				out.Note("\".\" scans the whole repository; ignoring extra path argument(s): %v", extras)
			}

			return &analysis.AllProvider{}
		}

		return &analysis.MultiFileProvider{Paths: files}
	}

	if staged {
		return &analysis.StagedProvider{}
	}

	if all {
		return &analysis.AllProvider{}
	}

	return &analysis.UncommittedProvider{}
}

func exitCodeForAnalysisError(err error) ExitCode {
	var driftErr *analysis.DriftDetectedError
	if errors.As(err, &driftErr) {
		return ExitDriftDetected
	}

	return ExitError
}
