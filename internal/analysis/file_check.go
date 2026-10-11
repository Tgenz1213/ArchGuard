package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type fileCheck struct {
	engine  *Engine
	run     *runState
	out     *output.Printer
	file    string
	content string
	// Baseline reads/writes compare against fullContent, not the possibly-partial content the LLM saw.
	fullContent string
	mode        string
	partial     bool
	findings    fileResult
}

func (e *Engine) checkFile(ctx context.Context, run *runState, file string) {
	fc := &fileCheck{engine: e, run: run, out: run.out.Group(file), file: file}
	defer fc.out.Flush()

	if !fc.load(ctx) {
		return
	}

	hits, ok := fc.candidates()
	if !ok {
		return
	}

	scored := len(hits) > 0

	hits, ok = fc.score(ctx, hits)
	if !ok {
		return
	}

	if len(hits) == 0 {
		fc.out.Debug("No relevant ADRs found.")
	}

	for _, hit := range hits {
		fc.checkADR(ctx, hit)
	}

	fc.findings.withoutADR = !fc.partial && len(hits) == 0
	// Top-K alone never empties a stage, so scored candidates that all dropped fell below a threshold.
	fc.findings.belowThreshold = fc.findings.withoutADR && scored
	fc.findings.judged = !fc.partial && fc.findings.checks > 0
	run.merge(&fc.findings)
}

func (fc *fileCheck) skip(reason string) {
	fc.out.Error("%s", reason)
	fc.run.addSkipped(fc.file, reason)
}

func (fc *fileCheck) load(ctx context.Context) bool {
	var err error

	fc.content, fc.fullContent, fc.mode, err = fc.engine.fetchContext(ctx, fc.file)
	if err != nil {
		fc.skip(fmt.Sprintf("reading file: %v", err))
		return false
	}

	fc.out.Debug("Context mode: %s", fc.mode)

	if fc.mode == "empty" {
		fc.out.Debug("No judgeable changes; skipping")
		return false
	}

	if fc.mode != "truncated" {
		return true
	}

	if fc.engine.CI && !fc.engine.UpdateBaseline {
		fc.out.Warn("truncated for analysis; in CI mode this is a warning, not a failure")
		fc.run.addSkipped(fc.file, "too large to analyze in full; CI mode skips it instead of failing")

		return false
	}

	fc.out.Warn("truncated for analysis; only the visible portion is checked")
	fc.run.addPartial(fc.file, "too large to analyze in full; only the visible portion was checked")
	fc.partial = true

	return true
}

func (fc *fileCheck) candidates() ([]stage.Candidate, bool) {
	source := fc.content
	if fc.engine.JudgeChange {
		source = fc.fullContent
	}

	hits, err := candidateSource{store: fc.engine.Store}.For(fc.file, source, fc.out)
	if err != nil {
		fc.skip(fmt.Sprintf("loading candidate ADRs: %v", err))
		return nil, false
	}

	return hits, true
}

func (fc *fileCheck) score(ctx context.Context, hits []stage.Candidate) ([]stage.Candidate, bool) {
	query := &queryFile{path: fc.file, content: fc.content, provider: fc.engine.Content, updateBaseline: fc.engine.UpdateBaseline}

	for i, st := range fc.run.stages {
		var err error

		hits, err = fc.run.telemetry.Apply(ctx, i, query, fc.out, hits)
		if err != nil {
			fc.failStage(st, err)
			return nil, false
		}
	}

	return hits, true
}

func (fc *fileCheck) failStage(st stage.Stage, err error) {
	if !st.FailOnError {
		fc.skip(scoringErrorMessage(err))
		return
	}

	failure := newStageFailure(st.Name, fc.file, err)
	fc.out.Error("%s", failureMessage(failure))
	fc.run.addStageFailure(failure)
}

func (fc *fileCheck) checkADR(ctx context.Context, hit stage.Candidate) {
	input := inference.DriftInput{ADRContent: hit.ADR.Content, CodeContext: fc.content, Filename: fc.file, Diff: fc.engine.JudgeChange}

	result, cached, err := fc.analyze(ctx, hit, input)
	if err != nil {
		fc.out.Debug("%s", checkOutcomeLine(hit, "failed", false))
		fc.out.Warn("LLM analysis failed: %v", err)
		fc.findings.failedChecks = append(fc.findings.failedChecks, output.FailedCheck{File: fc.file, ADRID: hit.ADR.ID, Title: hit.ADR.Title, Reason: err.Error()})

		return
	}

	fc.findings.checks++

	outcome := "compliant"
	if result.Violation {
		outcome = "violation"
	}

	fc.out.Debug("%s", checkOutcomeLine(hit, outcome, cached))

	if result.Violation {
		fc.judge(ctx, hit, input, result)
	}
}

func (fc *fileCheck) analyze(ctx context.Context, hit stage.Candidate, input inference.DriftInput) (result *inference.AnalysisResult, cached bool, err error) {
	e := fc.engine

	systemPrompt := e.Config.LLM.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = inference.DefaultSystemPrompt
	}

	cacheKey := cache.ComputeAnalysisKey(cache.AnalysisKeyInput{
		ModelName:          e.Config.LLM.Model,
		ADRContent:         hit.ADR.Content,
		FileContent:        fc.content,
		SystemPrompt:       systemPrompt,
		UserPromptTemplate: inference.PromptTemplate(e.JudgeChange),
	})

	if e.Cache != nil {
		if hitRes, found, getErr := e.Cache.Get(cacheKey); getErr == nil && found {
			return hitRes, true, nil
		}
	}

	result, err = inference.AnalyzeDrift(ctx, e.Chat, input, systemPrompt)
	if err != nil {
		return nil, false, err
	}

	if e.Cache != nil {
		if putErr := e.Cache.Put(cacheKey, result); putErr != nil {
			fc.out.Debug("Failed to cache analysis result: %v", putErr)
		}
	}

	return result, false, nil
}

func (fc *fileCheck) judge(ctx context.Context, hit stage.Candidate, input inference.DriftInput, result *inference.AnalysisResult) {
	record, keep := fc.locate(hit, result)
	if !keep {
		return
	}

	switch {
	case fc.engine.UpdateBaseline:
		fc.recordBaselineEntry(hit, result, record)
	case fc.engine.Baseline.IsSuppressed(hit.ADR.ID, fc.file, fc.fullContent):
		fc.out.Info("%s", violationLine(record, true))
		fc.findings.baselined++
	default:
		fc.reportViolation(ctx, hit, input, result, record)
	}
}

func (fc *fileCheck) locate(hit stage.Candidate, result *inference.AnalysisResult) (Violation, bool) {
	record := Violation{
		File:       fc.file,
		ADRID:      hit.ADR.ID,
		ADRTitle:   hit.ADR.Title,
		Reasoning:  result.Reasoning,
		QuotedCode: result.QuotedCode,
	}

	// Checked against the escaped content, which is what the LLM saw.
	escapedContent := inference.EscapePromptDelimiter(fc.content)

	if !fc.engine.JudgeChange {
		record.Line = fc.engine.findLineNumber(escapedContent, result.QuotedCode)
		record.lineInDiff = fc.mode == "diff"
		record.unverified = result.QuotedCode != "" && !strings.Contains(escapedContent, result.QuotedCode)

		return record, true
	}

	where, line := locateQuote(parseDiff(escapedContent), result.QuotedCode)
	if where == placedInContext {
		fc.out.Debug("Dropping a finding in unchanged code: %s", violationLine(record, false))

		return record, false
	}

	record.Line = line
	record.unverified = where == placedNowhere

	return record, true
}

func (fc *fileCheck) recordBaselineEntry(hit stage.Candidate, result *inference.AnalysisResult, record Violation) {
	e := fc.engine

	reason := e.BaselineReason
	if reason == "" {
		reason = e.Baseline.ReasonFor(hit.ADR.ID, fc.file)
	}

	fc.out.Info("%s", violationLine(record, false))

	// A QuotedCode that can't match the file would suppress nothing, so skip it.
	if result.QuotedCode != "" && !strings.Contains(fc.fullContent, result.QuotedCode) {
		fc.out.Warn("quoted code not found verbatim in file; skipping baseline entry")
		fc.findings.unrecorded = append(fc.findings.unrecorded, output.UnrecordedViolation{File: fc.file, ADRID: hit.ADR.ID, Title: hit.ADR.Title, Reason: "the quoted code was not found verbatim in the file"})

		return
	}

	fc.findings.entries = append(fc.findings.entries, baseline.Entry{
		ADRID:      hit.ADR.ID,
		File:       fc.file,
		QuotedCode: result.QuotedCode,
		Reason:     reason,
	})
	fc.findings.recorded = append(fc.findings.recorded, output.RecordedEntry{File: fc.file, ADRID: hit.ADR.ID, Title: hit.ADR.Title, Line: record.Line, Reason: reason})
}

func (fc *fileCheck) reportViolation(ctx context.Context, hit stage.Candidate, input inference.DriftInput, result *inference.AnalysisResult, record Violation) {
	if fc.engine.SuggestFixes && !record.unverified {
		record.Suggestion = fc.suggestFix(ctx, hit, input, result)
	}

	fc.out.Info("%s", violationLine(record, false))
	fc.findings.violations++
	fc.findings.records = append(fc.findings.records, record)
}

func (fc *fileCheck) suggestFix(ctx context.Context, hit stage.Candidate, input inference.DriftInput, result *inference.AnalysisResult) string {
	e := fc.engine

	key := cache.ComputeSuggestionKey(cache.SuggestionKeyInput{
		ModelName:                e.Config.LLM.Model,
		ADRContent:               hit.ADR.Content,
		FileContent:              fc.content,
		Filename:                 fc.file,
		Reasoning:                result.Reasoning,
		QuotedCode:               result.QuotedCode,
		SuggestionSystemPrompt:   inference.SuggestionSystemPrompt,
		SuggestionPromptTemplate: inference.SuggestionPrompt,
	})

	if e.Cache != nil {
		if cached, found, err := e.Cache.GetSuggestion(key); err == nil && found && cached != "" {
			return cached
		}
	}

	suggestion, err := inference.SuggestRemediation(ctx, e.Chat, input, *result)
	if err != nil {
		fc.out.Warn("suggestion generation failed: %v", err)
		return ""
	}

	if suggestion == "" {
		fc.out.Warn("suggestion generation returned an empty suggestion")
		return ""
	}

	if e.Cache != nil {
		if err := e.Cache.PutSuggestion(key, suggestion); err != nil {
			fc.out.Debug("Failed to cache suggestion: %v", err)
		}
	}

	return suggestion
}
