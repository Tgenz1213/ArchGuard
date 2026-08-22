package analysis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"golang.org/x/sync/errgroup"
)

// Engine coordinates the analysis of source files against ADRs using LLM providers.
type Engine struct {
	Config *config.Config
	Store  index.VectorStore
	// Provider handles Chat and CountTokens. It also handles CreateEmbedding
	// unless EmbedProvider is set.
	Provider llm.Provider
	// EmbedProvider, if set, handles CreateEmbedding instead of Provider.
	// See docs/arch/0004-decoupled-chat-and-embedding-providers.md.
	EmbedProvider llm.Provider
	Content       ContentProvider
	Debug         bool
	CI            bool // CI-safe mode (Warn-Open behavior)
	Cache         *cache.Cache
	// Baseline, if set, suppresses violations it already contains. nil means unused.
	Baseline *baseline.Baseline
	// UpdateBaseline, when true, bypasses Baseline and collects a fresh
	// snapshot into CollectedBaseline instead.
	UpdateBaseline bool
	// CollectedBaseline is populated by Run when UpdateBaseline is true; cli.go saves it.
	CollectedBaseline *baseline.Baseline
	// SkippedFiles is populated by Run when UpdateBaseline is true: the count
	// of files skipped due to per-file errors (fetchContext/CreateEmbedding failures).
	SkippedFiles int
}

// ErrDriftDetected identifies analysis results that contain architectural violations.
var ErrDriftDetected = errors.New("architectural drift detected")

// DriftDetectedError reports the number of architectural violations found.
type DriftDetectedError struct {
	Count int
}

func (e *DriftDetectedError) Error() string {
	return fmt.Sprintf("found %d architectural violations", e.Count)
}

func (e *DriftDetectedError) Is(target error) bool {
	return target == ErrDriftDetected
}

// NewEngine initializes a new analysis engine with a local cache.
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

// embedProvider returns EmbedProvider if set, otherwise Provider.
func (e *Engine) embedProvider() llm.Provider {
	if e.EmbedProvider != nil {
		return e.EmbedProvider
	}
	return e.Provider
}

// Log prints debug information if the engine is in debug mode.
func (e *Engine) Log(format string, args ...interface{}) {
	if e.Debug {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// Info prints standard informational messages.
func (e *Engine) Info(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// Run executes the analysis pipeline across all files provided by the ContentProvider.
func (e *Engine) Run(ctx context.Context) error {
	files, err := e.Content.GetFiles()
	if err != nil {
		return err
	}

	var (
		violations       int
		baselinedCount   int
		skippedFiles     int
		collectedEntries []baseline.Entry
		mu               sync.Mutex
	)

	concurrency := e.Config.Analysis.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	var g errgroup.Group
	g.SetLimit(concurrency)

	for _, file := range files {
		if e.shouldExclude(file) {
			continue
		}

		file := file
		g.Go(func() error {
			// buffer output to ensure atomic printing per file
			var sb strings.Builder

			if e.Debug {
				fmt.Fprintf(&sb, "Analyzing %s...\n", file)
			}

			content, fullContent, diffMode, err := e.fetchContext(ctx, file)
			if err != nil {
				fmt.Fprintf(&sb, "Error reading file %s: %v\n", file, err)
				mu.Lock()
				fmt.Print(sb.String())
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
				fmt.Print(sb.String())
				mu.Unlock()
				return nil
			}

			if diffMode == "truncated" && e.UpdateBaseline {
				fmt.Fprintf(&sb, "  Warning: %s was truncated for the baseline scan; only the visible portion was captured.\n", file)
			}

			// Same reasoning as fetchContext above: a diff only covers the
			// uncommitted hunk, not the whole file --update-baseline needs.
			diffForEmbedding := content
			if !e.UpdateBaseline {
				if diff, err := e.Content.GetDiff(file); err == nil && diff != "" {
					diffForEmbedding = stripDiffMetadata(diff)
				}
			}

			if len(diffForEmbedding) > 6000 {
				diffForEmbedding = rollBackToNewline(truncateRuneSafe(diffForEmbedding, 6000))
			}

			embedding, err := e.embedProvider().CreateEmbedding(ctx, diffForEmbedding, llm.EmbeddingTaskQuery)
			if err != nil {
				fmt.Fprintf(&sb, "Error generating embedding for %s: %v\n", file, err)
				mu.Lock()
				fmt.Print(sb.String())
				skippedFiles++
				mu.Unlock()
				return nil
			}

			hits := e.Store.Search(embedding, e.Config.VectorStore.SimilarityThreshold, 3)
			if len(hits) == 0 {
				if e.Debug {
					fmt.Fprintf(&sb, "  No relevant ADRs found.\n")
				}
				mu.Lock()
				fmt.Print(sb.String())
				mu.Unlock()
				return nil
			}

			// Baseline reads/writes compare against the untruncated file,
			// not the possibly-partial content the LLM saw.
			baselineContent := fullContent

			localViolations := 0
			localBaselined := 0
			var localBaselineEntries []baseline.Entry
			for _, hit := range hits {
				if hit.ADR.Scope != "" && !matchGlob(hit.ADR.Scope, file) {
					continue
				}

				// Check for ignore directive (optimization: only check header)
				header := content
				if len(header) > 2000 {
					header = truncateRuneSafe(header, 2000)
				}
				if strings.Contains(header, fmt.Sprintf("archguard-ignore: %s", hit.ADR.ID)) {
					if e.Debug {
						fmt.Fprintf(&sb, "  Skipping ADR %s (Suppressed)\n", hit.ADR.Title)
					}
					continue
				}

				if e.Debug {
					fmt.Fprintf(&sb, "  Checking against ADR: %s (%.2f)\n", hit.ADR.Title, hit.Score)
				}

				systemPrompt := e.Config.LLM.SystemPrompt
				if systemPrompt == "" {
					systemPrompt = llm.DefaultSystemPrompt
				}

				cacheKey := cache.ComputeAnalysisKey(e.Config.LLM.Model, hit.ADR.Content, content, systemPrompt, llm.ChatPrompt)

				var res *llm.AnalysisResult
				if e.Cache != nil {
					cachedRes, found, err := e.Cache.Get(cacheKey)
					if err == nil && found {
						// We can't log debug easily to sb properly unless we implement a custom logger on Engine
						// but skipping for now or just append
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
						continue
					}
					if e.Cache != nil {
						if err := e.Cache.Put(cacheKey, res); err != nil {
							e.Log("Failed to cache analysis result: %v", err)
						}
					}
				}

				if res.Violation {
					lineNum := e.findLineNumber(content, res.QuotedCode)
					switch {
					case e.UpdateBaseline:
						fmt.Fprintf(&sb, "    [VIOLATION] %s [Line %d]\n", hit.ADR.Title, lineNum)
						fmt.Fprintf(&sb, "    Reasoning: %s\n", res.Reasoning)
						if res.QuotedCode != "" {
							fmt.Fprintf(&sb, "    Code: %s\n", res.QuotedCode)
						}
						// A QuotedCode that won't match the file verbatim would
						// suppress nothing -- skip rather than write a dead entry.
						if res.QuotedCode == "" || strings.Contains(baselineContent, res.QuotedCode) {
							localBaselineEntries = append(localBaselineEntries, baseline.Entry{
								ADRID:      hit.ADR.ID,
								File:       file,
								QuotedCode: res.QuotedCode,
							})
						} else {
							fmt.Fprintf(&sb, "    Warning: quoted code not found verbatim in file; skipping baseline entry\n")
						}
					case e.Baseline.IsSuppressed(hit.ADR.ID, file, baselineContent):
						fmt.Fprintf(&sb, "    [BASELINED] %s [Line %d]\n", hit.ADR.Title, lineNum)
						fmt.Fprintf(&sb, "    Reasoning: %s\n", res.Reasoning)
						if res.QuotedCode != "" {
							fmt.Fprintf(&sb, "    Code: %s\n", res.QuotedCode)
						}
						localBaselined++
					default:
						fmt.Fprintf(&sb, "    [VIOLATION] %s [Line %d]\n", hit.ADR.Title, lineNum)
						fmt.Fprintf(&sb, "    Reasoning: %s\n", res.Reasoning)
						if res.QuotedCode != "" {
							fmt.Fprintf(&sb, "    Code: %s\n", res.QuotedCode)
						}
						localViolations++
					}
				}
			}

			mu.Lock()
			fmt.Print(sb.String())
			violations += localViolations
			baselinedCount += localBaselined
			if e.UpdateBaseline {
				collectedEntries = append(collectedEntries, localBaselineEntries...)
			}
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()

	if e.UpdateBaseline {
		b := baseline.New()
		for _, entry := range collectedEntries {
			b.Add(entry.ADRID, entry.File, entry.QuotedCode)
		}
		e.CollectedBaseline = b
		e.SkippedFiles = skippedFiles
		return nil
	}

	if e.Baseline != nil && (violations > 0 || baselinedCount > 0) {
		e.Info("%d new violation(s), %d baselined.", violations, baselinedCount)
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
		if matchGlob(pattern, path) {
			return true
		}
	}
	return false
}

// fetchContext returns the LLM content (maybe a diff/excerpt) alongside
// the untruncated fullContent, so callers needing both don't re-read the file.
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

// truncateToTokenLimit cuts content to at most maxTokens per the
// provider's own CountTokens, then rolls back to the nearest newline.
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

	// Smart Truncate: roll back to the nearest preceding newline character.
	if lastNewline := strings.LastIndex(candidate, "\n"); lastNewline != -1 {
		candidate = candidate[:lastNewline+1]
	}

	return candidate, nil
}

// clampRuneBoundary clamps cut into [0, len(s)] and, if it lands in the
// middle of a multi-byte UTF-8 rune, backs it up to the start of that rune.
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

// truncateRuneSafe cuts s to at most limit bytes without splitting a
// multi-byte UTF-8 rune.
func truncateRuneSafe(s string, limit int) string {
	return s[:clampRuneBoundary(s, limit)]
}

// rollBackToNewline trims s back to end at its last newline character, if
// any, so truncated content doesn't end mid-line.
func rollBackToNewline(s string) string {
	if lastNewline := strings.LastIndex(s, "\n"); lastNewline != -1 {
		return s[:lastNewline+1]
	}
	return s
}

// stripDiffMetadata strips unified-diff markup, leaving only code content,
// so an embedding compares code against ADR prose, not patch syntax.
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
			// preamble line (diff --git/index/---/+++), dropped
		case strings.HasPrefix(line, "diff --git "):
			// a second file's preamble in (unsupported) multi-file input
			inHunk = false
		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" marker, dropped
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

// hunkHeaderPattern matches a real unified diff hunk header, e.g.
// "@@ -12,7 +12,8 @@" (optionally followed by trailing function context).
var hunkHeaderPattern = regexp.MustCompile(`(?m)^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// isUnifiedDiff reports whether s looks like a real diff, not just content
// that happens to contain an "@@" line (e.g. a doc with an example hunk).
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
