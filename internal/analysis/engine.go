package analysis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/output"
	"golang.org/x/sync/errgroup"
)

type Engine struct {
	Config              *config.Config
	Store               index.VectorStore
	Chat                llm.Chatter
	Embed               llm.Embedder
	Content             ContentProvider
	Debug               bool
	CI                  bool
	Cache               *cache.Cache
	Baseline            *baseline.Baseline
	UpdateBaseline      bool
	BaselineReason      string
	CollectedBaseline   *baseline.Baseline
	SkippedFiles        int
	SkippedADRChecks    int
	JSONOutput          bool
	Out                 *output.Printer
	CollectedViolations []Violation
	CollectedStages     []stage.Stats
	StageFailures       []StageFailure
	// Off by default: adds one LLM call per reported violation.
	SuggestFixes bool
	Stages       []stage.Stage
}

type Violation struct {
	File       string `json:"file"`
	ADRID      string `json:"adr_id"`
	ADRTitle   string `json:"adr_title"`
	Line       int    `json:"line"`
	Reasoning  string `json:"reasoning"`
	QuotedCode string `json:"quoted_code"`
	Suggestion string `json:"suggestion,omitempty"`
}

type StageFailure struct {
	Stage string     `json:"stage"`
	File  string     `json:"file"`
	Kind  stage.Kind `json:"kind"`
	Error string     `json:"error"`
}

var ErrDriftDetected = errors.New("architectural drift detected")

type DriftDetectedError struct {
	Count int
}

func (e *DriftDetectedError) Error() string {
	return fmt.Sprintf("found %d architectural violations", e.Count)
}

func (e *DriftDetectedError) Is(target error) bool {
	return target == ErrDriftDetected
}

func NewEngine(cfg *config.Config, store index.VectorStore, chat llm.Chatter, embed llm.Embedder, content ContentProvider) *Engine {
	return &Engine{
		Config:  cfg,
		Store:   store,
		Chat:    chat,
		Embed:   embed,
		Content: content,
	}
}

func (e *Engine) printer() *output.Printer {
	if e.Out != nil {
		return e.Out
	}

	return output.New(nil, e.Debug)
}

func (e *Engine) Run(ctx context.Context) error {
	out := e.printer()

	stages := e.Stages
	if len(stages) == 0 {
		stages = []stage.Stage{stage.NewCosineStage(e.Store, e.Embed, e.Config.VectorStore.SimilarityThreshold, e.Config.Analysis.RelevantADRLimit())}
	}

	telemetry := stage.NewTelemetry(stages)

	files, err := e.Content.GetFiles(ctx)
	if err != nil {
		if e.JSONOutput {
			e.CollectedStages = telemetry.Stats()
		}

		return err
	}

	var (
		violations          int
		baselinedCount      int
		skippedFiles        int
		skippedADRChecks    int
		collectedEntries    []baseline.Entry
		collectedViolations []Violation
		stageFailures       []StageFailure
		mu                  sync.Mutex
	)

	concurrency := e.Config.Analysis.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	var g errgroup.Group
	g.SetLimit(concurrency)

	_, explicitFiles := e.Content.(*MultiFileProvider)

	for _, file := range files {
		if e.shouldExclude(file) {
			if explicitFiles && file != baseline.Path {
				out.Debug("Skipping %s: explicitly requested but matches exclude_patterns", file)
			}

			continue
		}

		file := file
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}

			fileOut := out.Group(file)
			defer fileOut.Flush()

			content, fullContent, diffMode, err := e.fetchContext(ctx, file)
			if err != nil {
				fileOut.Error("reading file: %v", err)
				mu.Lock()
				skippedFiles++
				mu.Unlock()
				return nil
			}

			fileOut.Debug("Context mode: %s", diffMode)

			if diffMode == "truncated" && e.CI && !e.UpdateBaseline {
				fileOut.Warn("truncated for analysis; in CI mode this is a warning, not a failure")
				return nil
			}

			if diffMode == "truncated" && e.UpdateBaseline {
				fileOut.Warn("truncated for the baseline scan; only the visible portion was captured")
			}

			hits, err := candidateSource{store: e.Store}.For(file, content, fileOut)
			if err != nil {
				fileOut.Error("loading candidate ADRs: %v", err)
				mu.Lock()
				skippedFiles++
				mu.Unlock()
				return nil
			}

			query := &queryFile{path: file, content: content, provider: e.Content, updateBaseline: e.UpdateBaseline}
			for i, st := range stages {
				hits, err = telemetry.Apply(ctx, i, query, fileOut, hits)
				if err != nil {
					mu.Lock()
					if st.FailOnError {
						failure := newStageFailure(st.Name, file, err)
						fileOut.Error("%s", failureMessage(failure))
						stageFailures = append(stageFailures, failure)
					} else {
						fileOut.Error("%s", scoringErrorMessage(err))
						skippedFiles++
					}

					mu.Unlock()
					return nil
				}
			}

			if len(hits) == 0 {
				fileOut.Debug("No relevant ADRs found.")
				return nil
			}

			// Baseline reads/writes compare against the untruncated file,
			// not the possibly-partial content the LLM saw.
			baselineContent := fullContent

			localViolations := 0
			localBaselined := 0
			localSkippedADRChecks := 0
			var localBaselineEntries []baseline.Entry
			var localViolationRecords []Violation
			for _, hit := range hits {
				fileOut.Debug("Checking against ADR: %s (%.2f)", hit.ADR.Title, hit.Score)

				systemPrompt := e.Config.LLM.SystemPrompt
				if systemPrompt == "" {
					systemPrompt = llm.DefaultSystemPrompt
				}

				cacheKey := cache.ComputeAnalysisKey(cache.AnalysisKeyInput{
					ModelName:          e.Config.LLM.Model,
					ADRContent:         hit.ADR.Content,
					FileContent:        content,
					SystemPrompt:       systemPrompt,
					UserPromptTemplate: llm.ChatPrompt,
				})

				driftInput := llm.DriftInput{ADRContent: hit.ADR.Content, CodeContext: content, Filename: file}

				var res *llm.AnalysisResult

				if e.Cache != nil {
					cachedRes, found, err := e.Cache.Get(cacheKey)
					if err == nil && found {
						fileOut.Debug("Cache Hit for %s", hit.ADR.Title)
						res = cachedRes
					}
				}

				if res == nil {
					fileOut.Debug("Cache Miss. Calling LLM...")

					res, err = llm.AnalyzeDrift(ctx, e.Chat, driftInput, systemPrompt)
					if err != nil {
						fileOut.Warn("LLM analysis failed: %v", err)
						localSkippedADRChecks++
						continue
					}

					if e.Cache != nil {
						if err := e.Cache.Put(cacheKey, res); err != nil {
							fileOut.Debug("Failed to cache analysis result: %v", err)
						}
					}
				}

				if res.Violation {
					// Verified against the escaped form of content -- what the LLM
					// actually saw (llm.EscapePromptDelimiter), not the raw file.
					escapedContent := llm.EscapePromptDelimiter(content)
					lineNum := e.findLineNumber(escapedContent, res.QuotedCode)
					verified := res.QuotedCode == "" || strings.Contains(escapedContent, res.QuotedCode)
					switch {
					case e.UpdateBaseline:
						reason := e.BaselineReason
						if reason == "" {
							reason = e.Baseline.ReasonFor(hit.ADR.ID, file)
						}

						fileOut.Violation(output.Violation{
							File:           file,
							Line:           lineNum,
							Verified:       verified,
							ADRID:          hit.ADR.ID,
							Title:          hit.ADR.Title,
							Reasoning:      res.Reasoning,
							Code:           res.QuotedCode,
							BaselineReason: reason,
						})
						// A QuotedCode that won't match the file verbatim would
						// suppress nothing -- skip rather than write a dead entry.
						if res.QuotedCode == "" || strings.Contains(baselineContent, res.QuotedCode) {
							localBaselineEntries = append(localBaselineEntries, baseline.Entry{
								ADRID:      hit.ADR.ID,
								File:       file,
								QuotedCode: res.QuotedCode,
								Reason:     reason,
							})
						} else {
							fileOut.Warn("quoted code not found verbatim in file; skipping baseline entry")
						}
					case e.Baseline.IsSuppressed(hit.ADR.ID, file, baselineContent):
						fileOut.Violation(output.Violation{
							File:           file,
							Line:           lineNum,
							Verified:       verified,
							Baselined:      true,
							ADRID:          hit.ADR.ID,
							Title:          hit.ADR.Title,
							Reasoning:      res.Reasoning,
							Code:           res.QuotedCode,
							BaselineReason: e.Baseline.ReasonFor(hit.ADR.ID, file),
						})
						localBaselined++
					default:
						var suggestion string

						if e.SuggestFixes && verified {
							suggestionKey := cache.ComputeSuggestionKey(cache.SuggestionKeyInput{
								ModelName:                e.Config.LLM.Model,
								ADRContent:               hit.ADR.Content,
								FileContent:              content,
								Filename:                 file,
								Reasoning:                res.Reasoning,
								QuotedCode:               res.QuotedCode,
								SuggestionSystemPrompt:   llm.SuggestionSystemPrompt,
								SuggestionPromptTemplate: llm.SuggestionPrompt,
							})
							if e.Cache != nil {
								if cached, found, err := e.Cache.GetSuggestion(suggestionKey); err == nil && found {
									suggestion = cached
								}
							}

							if suggestion == "" {
								s, sErr := llm.SuggestRemediation(ctx, e.Chat, driftInput, *res)
								switch {
								case sErr != nil:
									fileOut.Warn("suggestion generation failed: %v", sErr)
								case s == "":
									fileOut.Warn("suggestion generation returned an empty suggestion")
								default:
									suggestion = s
									if e.Cache != nil {
										if err := e.Cache.PutSuggestion(suggestionKey, s); err != nil {
											fileOut.Debug("Failed to cache suggestion: %v", err)
										}
									}
								}
							}
						}

						fileOut.Violation(output.Violation{
							File:       file,
							Line:       lineNum,
							Verified:   verified,
							ADRID:      hit.ADR.ID,
							Title:      hit.ADR.Title,
							Reasoning:  res.Reasoning,
							Code:       res.QuotedCode,
							Suggestion: suggestion,
						})
						localViolations++

						if e.JSONOutput {
							localViolationRecords = append(localViolationRecords, Violation{
								File:       file,
								ADRID:      hit.ADR.ID,
								ADRTitle:   hit.ADR.Title,
								Line:       lineNum,
								Reasoning:  res.Reasoning,
								QuotedCode: res.QuotedCode,
								Suggestion: suggestion,
							})
						}
					}
				}
			}

			mu.Lock()
			violations += localViolations
			baselinedCount += localBaselined
			skippedADRChecks += localSkippedADRChecks

			if e.UpdateBaseline {
				collectedEntries = append(collectedEntries, localBaselineEntries...)
			}

			collectedViolations = append(collectedViolations, localViolationRecords...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Checked before any summary or baseline snapshot: a partial scan must never look complete.
	if err := ctx.Err(); err != nil {
		return err
	}

	e.SkippedFiles = skippedFiles
	e.SkippedADRChecks = skippedADRChecks
	sort.Slice(stageFailures, func(i, j int) bool {
		if stageFailures[i].File != stageFailures[j].File {
			return stageFailures[i].File < stageFailures[j].File
		}

		return stageFailures[i].Stage < stageFailures[j].Stage
	})

	e.StageFailures = stageFailures
	if e.JSONOutput {
		if collectedViolations == nil {
			collectedViolations = []Violation{}
		}

		e.CollectedViolations = collectedViolations
		e.CollectedStages = telemetry.Stats()
	}

	if e.UpdateBaseline {
		b := baseline.New()
		for _, entry := range collectedEntries {
			b.Add(entry)
		}

		e.CollectedBaseline = b
		return nil
	}

	if (e.Baseline != nil && (violations > 0 || baselinedCount > 0)) || skippedFiles > 0 || skippedADRChecks > 0 {
		out.Result("%d new violation(s), %d baselined, %d file(s) skipped due to errors, %d ADR check(s) skipped due to LLM errors.", violations, baselinedCount, skippedFiles, skippedADRChecks)
	}

	if violations > 0 {
		return &DriftDetectedError{Count: violations}
	}

	return nil
}

func (e *Engine) shouldExclude(path string) bool {
	// Always excluded, not conditional on exclude_patterns: the baseline
	// file quotes prior violations and must never be scanned as source.
	if path == baseline.Path {
		return true
	}

	for _, pattern := range e.Config.Analysis.ExcludePatterns {
		if index.MatchGlob(pattern, path) {
			return true
		}
	}

	return false
}

// fetchContext also returns the untruncated content so callers needing both don't re-read the file.
func (e *Engine) fetchContext(ctx context.Context, path string) (content, fullContent, mode string, err error) {
	maxTokens := e.Config.LLM.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8000
	}

	fullContent, err = e.Content.GetContent(ctx, path)
	if err != nil {
		return "", "", "", err
	}

	totalTokens, err := e.Chat.CountTokens(ctx, fullContent)
	if err != nil {
		return "", "", "", fmt.Errorf("counting tokens for %s: %w", path, err)
	}

	if totalTokens <= maxTokens {
		return fullContent, fullContent, "full", nil
	}

	// A diff only covers the uncommitted-vs-HEAD hunk, which can't satisfy
	// --update-baseline's whole-file snapshot contract (docs/arch/0006).
	if !e.UpdateBaseline {
		diff, err := e.Content.GetDiff(ctx, path)
		if err == nil && diff != "" {
			return diff, fullContent, "diff", nil
		}
	}

	truncated, err := e.truncateToTokenLimit(ctx, fullContent, totalTokens, maxTokens)
	if err != nil {
		return "", "", "", fmt.Errorf("truncating content for %s: %w", path, err)
	}

	return truncated, fullContent, "truncated", nil
}

func (e *Engine) truncateToTokenLimit(ctx context.Context, content string, totalTokens, maxTokens int) (string, error) {
	bytesPerToken := float64(len(content)) / float64(totalTokens)
	cut := clampRuneBoundary(content, int(float64(maxTokens)*bytesPerToken))
	candidate := content[:cut]

	const maxProportionalAttempts = 5
	fits := false
	for attempt := 0; attempt < maxProportionalAttempts && cut > 0; attempt++ {
		n, err := e.Chat.CountTokens(ctx, candidate)
		if err != nil {
			return "", err
		}

		if n <= maxTokens {
			fits = true
			break
		}

		cut = clampRuneBoundary(content, int(float64(cut)*float64(maxTokens)/float64(n)))
		candidate = content[:cut]
	}

	for !fits && cut > 0 {
		// Halve before measuring, not after: the entry candidate is already
		// known to exceed maxTokens, so re-measuring it would be redundant.
		cut = clampRuneBoundary(content, cut/2)
		candidate = content[:cut]

		n, err := e.Chat.CountTokens(ctx, candidate)
		if err != nil {
			return "", err
		}

		if n <= maxTokens {
			fits = true
		}
	}

	if lastNewline := strings.LastIndex(candidate, "\n"); lastNewline != -1 {
		candidate = candidate[:lastNewline+1]
	}

	return candidate, nil
}

func clampRuneBoundary(s string, cut int) int {
	if cut < 0 {
		return 0
	}

	if cut >= len(s) {
		return len(s)
	}

	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return cut
}

func truncateRuneSafe(s string, limit int) string {
	return s[:clampRuneBoundary(s, limit)]
}

func rollBackToNewline(s string) string {
	if lastNewline := strings.LastIndex(s, "\n"); lastNewline != -1 {
		return s[:lastNewline+1]
	}

	return s
}

// Patch syntax would skew the embedding away from code-vs-ADR-prose similarity.
func stripDiffMetadata(s string) string {
	if !isUnifiedDiff(s) {
		return s
	}

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	inHunk := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case !inHunk:
		case strings.HasPrefix(line, "diff --git "):
			// a second file's preamble in (unsupported) multi-file input
			inHunk = false
		case strings.HasPrefix(line, "\\"):
			// git's "\ No newline at end of file" marker
		default:
			if line != "" {
				out = append(out, line[1:])
			} else {
				out = append(out, "")
			}
		}
	}

	return strings.Join(out, "\n")
}

var hunkHeaderPattern = regexp.MustCompile(`(?m)^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// Requires a git header too, so a doc containing an example "@@" hunk isn't mistaken for a diff.
func isUnifiedDiff(s string) bool {
	hasGitHeader := strings.Contains("\n"+s, "\ndiff --git ")
	return hasGitHeader && hunkHeaderPattern.MatchString(s)
}

func (e *Engine) findLineNumber(content, quote string) int {
	if quote == "" {
		return 0
	}

	idx := strings.Index(content, quote)
	if idx == -1 {
		return 0
	}

	lines := strings.Split(content[:idx], "\n")
	return len(lines)
}

func newStageFailure(name, file string, err error) StageFailure {
	failure := StageFailure{Stage: name, File: file, Error: err.Error()}

	var stageErr *stage.Error
	if errors.As(err, &stageErr) {
		failure.Kind = stageErr.Kind
	}

	return failure
}

func failureMessage(f StageFailure) string {
	return fmt.Sprintf("stage %s failed (%s): %s", f.Stage, f.Kind, f.Error)
}

func scoringErrorMessage(err error) string {
	var stageErr *stage.Error
	if errors.As(err, &stageErr) {
		return fmt.Sprintf("%s: %v", stageErr.Action, stageErr.Err)
	}

	return fmt.Sprintf("scoring candidates: %v", err)
}
