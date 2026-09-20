package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"golang.org/x/sync/errgroup"
)

type Engine struct {
	Config   *config.Config
	Store    index.VectorStore
	Provider llm.Provider
	// Claude has no embeddings API; see docs/arch/0004-decoupled-chat-and-embedding-providers.md.
	EmbedProvider       llm.Embedder
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
	Writer              io.Writer
	CollectedViolations []Violation
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

func NewEngine(cfg *config.Config, store index.VectorStore, provider llm.Provider, content ContentProvider, debug bool, ci bool) *Engine {
	c, _ := cache.NewCache(".")

	return &Engine{
		Config:   cfg,
		Store:    store,
		Provider: provider,
		Content:  content,
		Debug:    debug,
		CI:       ci,
		Cache:    c,
	}
}

func (e *Engine) embedProvider() llm.Embedder {
	if e.EmbedProvider != nil {
		return e.EmbedProvider
	}
	return e.Provider
}

func (e *Engine) Log(format string, args ...interface{}) {
	if e.Debug {
		_, _ = fmt.Fprintf(e.writer(), "[DEBUG] "+format+"\n", args...)
	}
}

func (e *Engine) Info(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(e.writer(), format+"\n", args...)
}

func (e *Engine) writer() io.Writer {
	if e.Writer != nil {
		return e.Writer
	}
	return os.Stdout
}

func (e *Engine) Run(ctx context.Context) error {
	files, err := e.Content.GetFiles()
	if err != nil {
		return err
	}

	var (
		violations          int
		baselinedCount      int
		skippedFiles        int
		skippedADRChecks    int
		collectedEntries    []baseline.Entry
		collectedViolations []Violation
		mu                  sync.Mutex
	)

	concurrency := e.Config.Analysis.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	topKADRs := e.Config.Analysis.MaxRelevantADRs
	if topKADRs <= 0 {
		topKADRs = 3
	}

	stages := e.Stages
	if len(stages) == 0 {
		stages = []stage.Stage{stage.NewCosineStage(e.Store, e.embedProvider(), e.Config.VectorStore.SimilarityThreshold, topKADRs)}
	}

	var g errgroup.Group
	g.SetLimit(concurrency)

	_, explicitFiles := e.Content.(*MultiFileProvider)

	for _, file := range files {
		if e.shouldExclude(file) {
			if explicitFiles && file != baseline.Path {
				e.Log("Skipping %s: explicitly requested but matches exclude_patterns", file)
			}
			continue
		}

		file := file
		g.Go(func() error {
			// Buffered so each file's output prints atomically.
			var sb strings.Builder

			if e.Debug {
				fmt.Fprintf(&sb, "Analyzing %s...\n", file)
			}

			content, fullContent, diffMode, err := e.fetchContext(ctx, file)
			if err != nil {
				fmt.Fprintf(&sb, "Error reading file %s: %v\n", file, err)
				mu.Lock()
				_, _ = fmt.Fprint(e.writer(), sb.String())
				skippedFiles++
				mu.Unlock()
				return nil
			}

			if e.Debug {
				fmt.Fprintf(&sb, "  Context mode: %s\n", diffMode)
			}

			if diffMode == "truncated" && e.CI && !e.UpdateBaseline {
				fmt.Fprintf(&sb, "  [WARN-OPEN] File %s was truncated for analysis. In CI mode this is treated as a warning (no failure).\n", file)
				mu.Lock()
				_, _ = fmt.Fprint(e.writer(), sb.String())
				mu.Unlock()
				return nil
			}

			if diffMode == "truncated" && e.UpdateBaseline {
				fmt.Fprintf(&sb, "  Warning: %s was truncated for the baseline scan; only the visible portion was captured.\n", file)
			}

			debug := stage.NoDebug
			if e.Debug {
				debug = stage.NewDebug(&sb)
			}

			hits, err := candidateSource{store: e.Store}.For(file, content, debug)
			if err != nil {
				fmt.Fprintf(&sb, "Error loading candidate ADRs for %s: %v\n", file, err)
				mu.Lock()
				_, _ = fmt.Fprint(e.writer(), sb.String())
				skippedFiles++
				mu.Unlock()
				return nil
			}
			query := &queryFile{path: file, content: content, provider: e.Content, updateBaseline: e.UpdateBaseline}
			for _, st := range stages {
				hits, err = st.Apply(ctx, query, debug, hits)
				if err != nil {
					sb.WriteString(scoringErrorMessage(file, err))
					mu.Lock()
					_, _ = fmt.Fprint(e.writer(), sb.String())
					skippedFiles++
					mu.Unlock()
					return nil
				}
			}

			if len(hits) == 0 {
				if e.Debug {
					fmt.Fprintf(&sb, "  No relevant ADRs found.\n")
				}
				mu.Lock()
				_, _ = fmt.Fprint(e.writer(), sb.String())
				mu.Unlock()
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
				if e.Debug {
					fmt.Fprintf(&sb, "  Checking against ADR: %s (%.2f)\n", hit.ADR.Title, hit.Score)
				}

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

				var res *llm.AnalysisResult
				if e.Cache != nil {
					cachedRes, found, err := e.Cache.Get(cacheKey)
					if err == nil && found {
						if e.Debug {
							fmt.Fprintf(&sb, "[DEBUG]   Cache Hit for %s\n", hit.ADR.Title)
						}
						res = cachedRes
					}
				}

				if res == nil {
					if e.Debug {
						fmt.Fprintf(&sb, "[DEBUG]   Cache Miss. Calling LLM...\n")
					}
					res, err = llm.AnalyzeDrift(ctx, e.Provider, hit.ADR.Content, content, file, systemPrompt)
					if err != nil {
						fmt.Fprintf(&sb, "    Warning: LLM analysis failed: %v\n", err)
						localSkippedADRChecks++
						continue
					}
					if e.Cache != nil {
						if err := e.Cache.Put(cacheKey, res); err != nil {
							e.Log("Failed to cache analysis result: %v", err)
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
						writeViolationOutput(&sb, violationOutput{
							Label:          "VIOLATION",
							Title:          hit.ADR.Title,
							LineNum:        lineNum,
							Reasoning:      res.Reasoning,
							QuotedCode:     res.QuotedCode,
							BaselineReason: reason,
						}, verified)
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
							fmt.Fprintf(&sb, "    Warning: quoted code not found verbatim in file; skipping baseline entry\n")
						}
					case e.Baseline.IsSuppressed(hit.ADR.ID, file, baselineContent):
						writeViolationOutput(&sb, violationOutput{
							Label:          "BASELINED",
							Title:          hit.ADR.Title,
							LineNum:        lineNum,
							Reasoning:      res.Reasoning,
							QuotedCode:     res.QuotedCode,
							BaselineReason: e.Baseline.ReasonFor(hit.ADR.ID, file),
						}, verified)
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
								s, sErr := llm.SuggestRemediation(ctx, e.Provider, hit.ADR.Content, content, file, res.Reasoning, res.QuotedCode)
								switch {
								case sErr != nil:
									fmt.Fprintf(&sb, "    Warning: suggestion generation failed: %v\n", sErr)
								case s == "":
									fmt.Fprintf(&sb, "    Warning: suggestion generation returned an empty suggestion\n")
								default:
									suggestion = s
									if e.Cache != nil {
										if err := e.Cache.PutSuggestion(suggestionKey, s); err != nil {
											e.Log("Failed to cache suggestion: %v", err)
										}
									}
								}
							}
						}
						writeViolationOutput(&sb, violationOutput{
							Label:      "VIOLATION",
							Title:      hit.ADR.Title,
							LineNum:    lineNum,
							Reasoning:  res.Reasoning,
							QuotedCode: res.QuotedCode,
							Suggestion: suggestion,
						}, verified)
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
			_, _ = fmt.Fprint(e.writer(), sb.String())
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

	_ = g.Wait()

	e.SkippedFiles = skippedFiles
	e.SkippedADRChecks = skippedADRChecks
	if e.JSONOutput {
		if collectedViolations == nil {
			collectedViolations = []Violation{}
		}
		e.CollectedViolations = collectedViolations
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
		e.Info("%d new violation(s), %d baselined, %d file(s) skipped due to errors, %d ADR check(s) skipped due to LLM errors.", violations, baselinedCount, skippedFiles, skippedADRChecks)
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

	fullContent, err = e.Content.GetContent(path)
	if err != nil {
		return "", "", "", err
	}

	totalTokens, err := e.Provider.CountTokens(ctx, fullContent)
	if err != nil {
		return "", "", "", fmt.Errorf("counting tokens for %s: %w", path, err)
	}
	if totalTokens <= maxTokens {
		return fullContent, fullContent, "full", nil
	}

	// A diff only covers the uncommitted-vs-HEAD hunk, which can't satisfy
	// --update-baseline's whole-file snapshot contract (docs/arch/0006).
	if !e.UpdateBaseline {
		diff, err := e.Content.GetDiff(path)
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
		n, err := e.Provider.CountTokens(ctx, candidate)
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
		n, err := e.Provider.CountTokens(ctx, candidate)
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

type violationOutput struct {
	Label          string
	Title          string
	LineNum        int
	Reasoning      string
	QuotedCode     string
	Suggestion     string
	BaselineReason string
}

func writeViolationOutput(sb *strings.Builder, v violationOutput, verified bool) {
	if verified {
		fmt.Fprintf(sb, "    [%s] %s [Line %d]\n", v.Label, v.Title, v.LineNum)
	} else {
		fmt.Fprintf(sb, "    [%s] %s [UNVERIFIED: quoted code not found in analyzed content]\n", v.Label, v.Title)
	}
	fmt.Fprintf(sb, "    Reasoning: %s\n", v.Reasoning)
	if v.QuotedCode != "" {
		fmt.Fprintf(sb, "    Code: %s\n", v.QuotedCode)
	}
	if v.Suggestion != "" {
		fmt.Fprintf(sb, "    Suggestion (unverified): %s\n", v.Suggestion)
	}
	if v.BaselineReason != "" {
		fmt.Fprintf(sb, "    Baseline Reason: %s\n", v.BaselineReason)
	}
}

func scoringErrorMessage(file string, err error) string {
	var stageErr *stage.Error
	if errors.As(err, &stageErr) {
		return fmt.Sprintf("Error %s for %s: %v\n", stageErr.Action, file, stageErr.Err)
	}
	return fmt.Sprintf("Error scoring candidates for %s: %v\n", file, err)
}
