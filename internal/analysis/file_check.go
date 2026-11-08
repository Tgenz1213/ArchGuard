package analysis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"github.com/tgenz1213/archguard/internal/telemetry"
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
	// The node that dropped this file's last candidates, when it was one that screens for violations.
	screenedOutBy string
	findings      fileResult
	suggestion    *suggestionCode
	diff          *string
}

// suggestionCode is the code every suggestion for the file is written from, sized once per file.
type suggestionCode struct {
	content string
	fits    bool
}

func (e *Engine) checkFile(ctx context.Context, run *runState, file string) {
	start := time.Now()
	defer func() { e.record(telemetry.FileChecked{File: file, Spent: time.Since(start)}) }()

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

	if fc.partial {
		fc.run.addPartial(fc.file, "too large to analyze in full; only the visible portion was checked")
	}

	fc.findings.withoutADR = !fc.partial && len(hits) == 0
	// Top-K alone never empties a node, so scored candidates that all dropped fell below a threshold.
	if fc.findings.withoutADR {
		fc.findings.screenedOutBy = fc.screenedOutBy
	}

	fc.findings.belowThreshold = fc.findings.withoutADR && scored && fc.screenedOutBy == ""
	fc.findings.judged = !fc.partial && fc.findings.checks > 0
	run.merge(&fc.findings)
}

func (fc *fileCheck) skip(reason string) {
	fc.out.Error("%s", reason)
	fc.run.addFailedFile(fc.file, reason)
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
	fc.partial = true

	return true
}

func (fc *fileCheck) candidates() ([]node.Candidate, bool) {
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

func (fc *fileCheck) score(ctx context.Context, hits []node.Candidate) ([]node.Candidate, bool) {
	query := &queryFile{
		path:           fc.file,
		content:        fc.content,
		provider:       fc.engine.Content,
		updateBaseline: fc.engine.UpdateBaseline,
		readSides:      func() ([]string, error) { return fc.askSides(ctx) },
	}

	for i, pipelineNode := range fc.run.nodes {
		var err error

		received := len(hits)

		hits, err = fc.run.telemetry.Apply(ctx, i, query, fc.out, hits)
		if skipped, ok := errors.AsType[*node.Skipped](err); ok {
			fc.out.Warn("node %s skipped, %d candidate(s) pass through unchanged: %s", pipelineNode.Name, len(hits), scoringErrorMessage(skipped.Err))
			continue
		}

		if node.KeyRejected(err) {
			// The run reports this once and stops; a line per file would repeat it.
			fc.run.rejectKey(newKeyRejectedError(pipelineNode.Name, err))
			return nil, false
		}

		if errors.Is(err, node.ErrUnscreenable) {
			fc.unscreenable(pipelineNode, err)
			return nil, false
		}

		if err != nil {
			fc.failNode(pipelineNode, err)
			return nil, false
		}

		if received > 0 && len(hits) == 0 && pipelineNode.Screens {
			fc.screenedOutBy = pipelineNode.Name
		}
	}

	return hits, true
}

// Jev never reads past llm.max_tokens, so a file over it is unscreenable rather than screened by half.
func (fc *fileCheck) askSides(ctx context.Context) ([]string, error) {
	e := fc.engine
	limit := tokenLimit(e.Config.LLM)

	if fc.mode == "diff" || (fc.mode != "truncated" && limit >= tokenLimit(e.judgeLLM())) {
		return sidesOf(fc.content, e.JudgeChange || fc.mode == "diff"), nil
	}

	content, mode, err := fc.fitAsk(ctx, limit)

	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case err != nil:
		return nil, err
	case mode == "truncated" || mode == "over":
		return nil, fmt.Errorf("%w: it is over llm.max_tokens (%d)", node.ErrUnscreenable, limit)
	}

	return sidesOf(content, e.JudgeChange || mode == "diff"), nil
}

// unscreenable skips the file whatever on_error says; outside --ci the skip counts toward a run that checked nothing.
func (fc *fileCheck) unscreenable(pipelineNode node.Node, err error) {
	reason := fmt.Sprintf("node %s can't screen it, so none of its ADRs were judged: %v", pipelineNode.Name, err)

	if fc.engine.CI && !fc.engine.UpdateBaseline {
		fc.out.Warn("%s", reason)
		fc.run.addSkipped(fc.file, reason)

		return
	}

	fc.skip(reason)
}

// fitAsk never truncates a whole file, since an ask node can't screen a truncated one.
func (fc *fileCheck) fitAsk(ctx context.Context, limit int) (content, mode string, err error) {
	if fc.engine.JudgeChange {
		return fc.engine.sizeContent(ctx, fc.file, fc.fullContent, limit)
	}

	content, mode, _, err = fc.engine.fitWhole(ctx, fc.engine.Chat, fc.file, fc.fullContent, limit)

	return content, mode, err
}

func (fc *fileCheck) failNode(pipelineNode node.Node, err error) {
	if !pipelineNode.FailOnError {
		fc.skip(scoringErrorMessage(err))
		return
	}

	failure := newNodeFailure(pipelineNode.Name, fc.file, err)
	fc.out.Error("%s", failureMessage(failure))
	fc.run.addNodeFailure(failure)
}

func (fc *fileCheck) checkADR(ctx context.Context, hit node.Candidate) {
	input := inference.DriftInput{ADRContent: hit.ADR.Content, CodeContext: fc.content, Filename: fc.file, Diff: fc.engine.JudgeChange}

	if fc.engine.DryRun {
		fc.project(ctx, hit, input)
		return
	}

	result, cached, err := fc.analyze(ctx, hit, input)
	if err != nil {
		fc.out.Debug("%s", checkOutcomeLine(hit, "failed", false))
		fc.out.Warn("LLM analysis failed: %v", err)
		fc.findings.failedChecks = append(fc.findings.failedChecks, output.FailedCheck{
			File:   fc.file,
			ADRID:  hit.ADR.ID,
			Title:  hit.ADR.Title,
			Reason: err.Error(),
		})

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

func (fc *fileCheck) analyze(ctx context.Context, hit node.Candidate, input inference.DriftInput) (result *inference.AnalysisResult, cached bool, err error) {
	e := fc.engine
	cacheKey := fc.analysisKey(hit)

	hitRes, found := fc.cachedVerdict(cacheKey)
	e.record(telemetry.CacheChecked{Role: telemetry.Judgment, Hit: found})

	if found {
		return hitRes, true, nil
	}

	result, retries, err := inference.AnalyzeDrift(ctx, e.Chat, input, e.systemPrompt())
	e.record(telemetry.RetriesSent{Role: telemetry.Judgment, Retries: retries})

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

func (fc *fileCheck) analysisKey(hit node.Candidate) string {
	e := fc.engine

	return cache.ComputeAnalysisKey(cache.AnalysisKeyInput{
		Provider:           e.judgeLLM().Provider,
		ModelName:          e.judgeLLM().Model,
		BaseURL:            e.judgeLLM().BaseURL,
		Temperature:        e.judgeLLM().Temperature,
		ADRContent:         hit.ADR.Content,
		FileContent:        fc.content,
		SystemPrompt:       e.systemPrompt(),
		UserPromptTemplate: inference.PromptTemplate(e.JudgeChange),
	})
}

func (fc *fileCheck) cachedVerdict(key string) (*inference.AnalysisResult, bool) {
	if fc.engine.Cache == nil {
		return nil, false
	}

	result, found, err := fc.engine.Cache.Get(key)
	if err != nil {
		fc.out.Warn("reading the cached verdict failed: %v", err)
		return nil, false
	}

	return result, found
}

func (fc *fileCheck) cachedSuggestion(key string) (string, bool) {
	if fc.engine.Cache == nil {
		return "", false
	}

	suggestion, found, err := fc.engine.Cache.GetSuggestion(key)
	if err != nil {
		fc.out.Warn("reading the cached suggestion failed: %v", err)
		return "", false
	}

	return suggestion, found && suggestion != ""
}

func (fc *fileCheck) judge(ctx context.Context, hit node.Candidate, input inference.DriftInput, result *inference.AnalysisResult) {
	record, keep := fc.locate(hit, result)
	if !keep {
		return
	}

	located := *result
	located.QuotedCode = record.QuotedCode
	result = &located

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

func (fc *fileCheck) locate(hit node.Candidate, result *inference.AnalysisResult) (Violation, bool) {
	// Checked against the escaped content, which is what the LLM saw.
	quote, where, line := fc.place(inference.EscapePromptDelimiter(fc.content), result.QuotedCode)

	record := Violation{
		File:       fc.file,
		ADRID:      hit.ADR.ID,
		ADRTitle:   hit.ADR.Title,
		Line:       line,
		Reasoning:  result.Reasoning,
		QuotedCode: quote,
		unverified: where == placedNowhere,
	}

	if where == placedInContext {
		fc.out.Debug("Dropping a finding in unchanged code: %s", violationLine(record, false))

		return record, false
	}

	return record, true
}

// A quote is unwrapped from markdown only when the unwrapped text is found, so backticks that belong to the code survive.
func (fc *fileCheck) place(content, quote string) (string, placement, int) {
	where, line := fc.find(content, quote)
	if where != placedNowhere {
		return quote, where, line
	}

	unwrapped := unwrapMarkdown(quote)
	if unwrapped == quote {
		return quote, where, line
	}

	if where, line := fc.find(content, unwrapped); where != placedNowhere {
		return unwrapped, where, line
	}

	return quote, placedNowhere, 0
}

func (fc *fileCheck) find(content, quote string) (placement, int) {
	if fc.engine.JudgeChange {
		return locateQuote(parseDiff(content), quote)
	}

	if fc.mode == "diff" {
		return locateInNewFile(parseDiff(content), quote)
	}

	if quote == "" || !strings.Contains(content, quote) {
		return placedNowhere, 0
	}

	return placedInFile, fc.engine.findLineNumber(content, quote)
}

func (fc *fileCheck) recordBaselineEntry(hit node.Candidate, result *inference.AnalysisResult, record Violation) {
	e := fc.engine

	reason := e.BaselineReason
	if reason == "" {
		reason = e.Baseline.ReasonFor(hit.ADR.ID, fc.file)
	}

	fc.out.Info("%s", violationLine(record, false))

	// A QuotedCode that can't match the file would suppress nothing, so skip it.
	if result.QuotedCode != "" && !strings.Contains(fc.fullContent, result.QuotedCode) {
		fc.out.Warn("quoted code not found verbatim in file; skipping baseline entry")
		fc.findings.unrecorded = append(fc.findings.unrecorded, output.UnrecordedViolation{
			File:   fc.file,
			ADRID:  hit.ADR.ID,
			Title:  hit.ADR.Title,
			Reason: "the quoted code was not found verbatim in the file",
		})

		return
	}

	fc.findings.entries = append(fc.findings.entries, baseline.Entry{
		ADRID:      hit.ADR.ID,
		File:       fc.file,
		QuotedCode: result.QuotedCode,
		Reason:     reason,
	})
	fc.findings.recorded = append(fc.findings.recorded, output.RecordedEntry{
		File:   fc.file,
		ADRID:  hit.ADR.ID,
		Title:  hit.ADR.Title,
		Line:   record.Line,
		Reason: reason,
	})
}

func (fc *fileCheck) reportViolation(ctx context.Context, hit node.Candidate, input inference.DriftInput, result *inference.AnalysisResult, record Violation) {
	if record.unverified {
		fc.out.Info("%s", violationLine(record, false))
		fc.findings.unverified = append(fc.findings.unverified, record)

		return
	}

	if fc.engine.SuggestFixes {
		record.Suggestion = fc.suggestFix(ctx, hit, input, result)
	}

	fc.out.Info("%s", violationLine(record, false))
	fc.findings.violations++
	fc.findings.records = append(fc.findings.records, record)
}

func (fc *fileCheck) suggestFix(ctx context.Context, hit node.Candidate, input inference.DriftInput, result *inference.AnalysisResult) string {
	e := fc.engine
	suggest := e.suggestLLM()

	// Keyed on what sizing starts from, so a cached suggestion costs no token count.
	key := cache.ComputeSuggestionKey(cache.SuggestionKeyInput{
		Diff:                     fc.fallbackDiff(ctx),
		Provider:                 suggest.Provider,
		ModelName:                suggest.Model,
		BaseURL:                  suggest.BaseURL,
		Temperature:              suggest.Temperature,
		MaxTokens:                tokenLimit(suggest),
		ADRContent:               hit.ADR.Content,
		FileContent:              fc.content,
		Filename:                 fc.file,
		Reasoning:                result.Reasoning,
		QuotedCode:               result.QuotedCode,
		SuggestionSystemPrompt:   inference.SuggestionSystemPrompt,
		SuggestionPromptTemplate: inference.SuggestionPrompt,
	})

	if cached, found := fc.cachedSuggestion(key); found {
		e.record(telemetry.CacheChecked{Role: telemetry.Suggestion, Hit: true})
		return cached
	}

	code := fc.suggestionCode(ctx)
	if !code.fits {
		return ""
	}

	e.record(telemetry.CacheChecked{Role: telemetry.Suggestion, Hit: false})

	input.CodeContext = code.content

	suggestion, retries, err := inference.SuggestRemediation(ctx, e.suggestionChat(), input, *result)
	e.record(telemetry.RetriesSent{Role: telemetry.Suggestion, Retries: retries})

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

// fallbackDiff is the diff a whole-file suggestion may be written from instead, read once per file.
func (fc *fileCheck) fallbackDiff(ctx context.Context) string {
	if fc.mode != "full" || fc.engine.UpdateBaseline {
		return ""
	}

	if fc.diff == nil {
		diff, err := fc.engine.Content.GetDiff(ctx, fc.file)
		if err != nil {
			diff = ""
		}

		fc.diff = &diff
	}

	return *fc.diff
}

func (fc *fileCheck) suggestsAsJudged() bool {
	judge, suggest := fc.engine.judgeLLM(), fc.engine.suggestLLM()

	// Only a diff fetchContext fell back to went uncounted against the judge's limit.
	return fc.mode != "diff" && suggest.Provider == judge.Provider && suggest.Model == judge.Model && tokenLimit(suggest) >= tokenLimit(judge)
}

func (fc *fileCheck) suggestionCode(ctx context.Context) suggestionCode {
	if fc.suggestion == nil {
		fc.suggestion = new(fc.sizeSuggestion(ctx))
	}

	return *fc.suggestion
}

func (fc *fileCheck) sizeSuggestion(ctx context.Context) suggestionCode {
	limit := tokenLimit(fc.engine.suggestLLM())

	if fc.suggestsAsJudged() {
		return suggestionCode{content: fc.content, fits: true}
	}

	content, fits, err := fc.fitSuggestion(ctx, limit)

	switch {
	case err != nil:
		fc.out.Warn("no suggestion: %v", err)
	case fits:
		return suggestionCode{content: content, fits: true}
	case fc.mode == "full":
		fc.out.Warn("no suggestion for %s: it is over the suggest model's max_tokens (%d) and has no diff that fits", fc.file, limit)
	default:
		fc.out.Warn("no suggestion for %s: what was judged is over the suggest model's max_tokens (%d)", fc.file, limit)
	}

	return suggestionCode{}
}

// A whole file falls back to its diff as fetchContext does, but is never truncated: a fix for half a file is no fix.
func (fc *fileCheck) fitSuggestion(ctx context.Context, limit int) (string, bool, error) {
	chat := fc.engine.suggestionChat()
	content := fc.content

	if fc.mode == "full" {
		fitted, mode, _, err := fc.engine.fitWhole(ctx, chat, fc.file, fc.content, limit)
		if err != nil || mode != "diff" {
			return fitted, mode == "full", err
		}

		content = fitted
	}

	tokens, err := chat.CountTokens(ctx, content)
	if err != nil {
		return "", false, fmt.Errorf("counting tokens for %s: %w", fc.file, err)
	}

	return content, tokens <= limit, nil
}
