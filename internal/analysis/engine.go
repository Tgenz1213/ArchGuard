package analysis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"golang.org/x/sync/errgroup"
)

// Engine coordinates the analysis of source files against ADRs using LLM providers.
type Engine struct {
	Config   *config.Config
	Store    index.VectorStore
	Provider llm.Provider
	Content  ContentProvider
	Debug    bool
	CI       bool // CI-safe mode (Warn-Open behavior)
	Cache    *cache.Cache
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
		violations int
		mu         sync.Mutex
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

			content, diffMode, err := e.fetchContext(ctx, file)
			if err != nil {
				fmt.Fprintf(&sb, "Error reading file %s: %v\n", file, err)
				mu.Lock()
				fmt.Print(sb.String())
				mu.Unlock()
				return nil
			}

			if e.Debug {
				fmt.Fprintf(&sb, "  Context mode: %s\n", diffMode)
			}

			if diffMode == "truncated" && e.CI {
				fmt.Fprintf(&sb, "  [WARN-OPEN] File %s was truncated for analysis. In CI mode this is treated as a warning (no failure).\n", file)
				mu.Lock()
				fmt.Print(sb.String())
				mu.Unlock()
				return nil
			}

			diffForEmbedding, err := e.Content.GetDiff(file)
			isDiff := err == nil && diffForEmbedding != ""
			if !isDiff {
				diffForEmbedding = content
			} else {
				diffForEmbedding = stripDiffMetadata(diffForEmbedding)
			}

			if len(diffForEmbedding) > 6000 {
				diffForEmbedding = rollBackToNewline(truncateRuneSafe(diffForEmbedding, 6000))
			}

			embedding, err := e.Provider.CreateEmbedding(ctx, diffForEmbedding, llm.EmbeddingTaskQuery)
			if err != nil {
				fmt.Fprintf(&sb, "Error generating embedding for %s: %v\n", file, err)
				mu.Lock()
				fmt.Print(sb.String())
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

			localViolations := 0
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
					fmt.Fprintf(&sb, "    [VIOLATION] %s [Line %d]\n", hit.ADR.Title, lineNum)
					fmt.Fprintf(&sb, "    Reasoning: %s\n", res.Reasoning)
					if res.QuotedCode != "" {
						fmt.Fprintf(&sb, "    Code: %s\n", res.QuotedCode)
					}
					localViolations++
				}
			}

			mu.Lock()
			fmt.Print(sb.String())
			violations += localViolations
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()

	if violations > 0 {
		return &DriftDetectedError{Count: violations}
	}

	return nil
}

func (e *Engine) shouldExclude(path string) bool {
	for _, pattern := range e.Config.Analysis.ExcludePatterns {
		if matchGlob(pattern, path) {
			return true
		}
	}
	return false
}

func (e *Engine) fetchContext(ctx context.Context, path string) (string, string, error) {
	maxTokens := e.Config.LLM.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8000
	}

	fullContent, err := e.Content.GetContent(path)
	if err != nil {
		return "", "", err
	}

	totalTokens, err := e.Provider.CountTokens(ctx, fullContent)
	if err != nil {
		return "", "", fmt.Errorf("counting tokens for %s: %w", path, err)
	}
	if totalTokens <= maxTokens {
		return fullContent, "full", nil
	}

	diff, err := e.Content.GetDiff(path)
	if err != nil || diff == "" {
		truncated, err := e.truncateToTokenLimit(ctx, fullContent, totalTokens, maxTokens)
		if err != nil {
			return "", "", fmt.Errorf("truncating content for %s: %w", path, err)
		}
		return truncated, "truncated", nil
	}
	return diff, "diff", nil
}

// truncateToTokenLimit cuts content down to at most maxTokens tokens,
// according to the provider's own CountTokens. It can't rely on
// encode/decode (only tiktoken supports that): instead it estimates a
// byte cutoff from the content's average bytes-per-token ratio, then
// verifies and shrinks that estimate via CountTokens, then rolls back to
// the nearest preceding newline so truncated files don't end mid-line.
//
// The average-density estimate converges in one or two calls when token
// density is roughly uniform, but can be a poor local predictor for
// content with a highly non-uniform density (e.g. a mostly-ASCII file
// ending in a dense CJK or base64 block), so a bounded proportional
// shrink alone isn't guaranteed to converge. Below that, an unconditional
// halving shrink guarantees termination -- integer halving strictly
// decreases the cut toward 0 -- so the returned content's token count is
// always <= maxTokens (down to the edge case of an empty result, 0
// tokens), not just a best-effort bound.
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

// stripDiffMetadata strips unified diff patch metadata (the diff --git/
// index/---/+++ preamble and @@ hunk headers) and the leading +/-/space
// marker from each hunk line, leaving the underlying code content -- so an
// embedding compares actual code, not patch syntax, against ADR prose.
//
// Input with no @@ hunk header is passed through unchanged rather than
// stripped line-by-line: that's the signal this isn't diff-shaped (e.g. it's
// whole-file content used as a fallback when there's no diff), and
// unconditionally stripping a leading space from every line would corrupt
// ordinary indented code.
//
// Everything before the first @@ is preamble and is dropped unconditionally,
// rather than matched line-by-line against "index "/"--- "/"+++ " prefixes:
// those prefixes can also occur as the first bytes of a genuine hunk line
// (marker + content), e.g. a removed SQL/Lua-style "-- comment" line
// becomes "--- comment" once the '-' marker is prepended. Re-checking for
// header prefixes after the hunk has started would misclassify that as the
// "--- a/file" header and silently drop it and every line after it.
//
// This function assumes s is a single-file diff, matching internal/git's
// only diff-producing calls (`git diff --unified=100 -- <one path>`), which
// is the only diff shape ArchGuard ever generates -- ADR scope matching,
// suppression comments, caching, and violation-line reporting are all
// file-scoped throughout the codebase, so mixing multiple files' diffs into
// one embedding isn't a design goal. A "diff --git " line reached mid-hunk
// still resets back to preamble (rather than being treated as hunk content
// like other header-shaped lines) purely as defense-in-depth: unlike the
// 4-character "--- "/"+++ " prefixes, "diff --git " is long and specific
// enough that a real code line coincidentally starting with it is not a
// realistic risk.
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

// isUnifiedDiff reports whether s looks like a real unified diff rather
// than ordinary content that happens to contain a line starting with "@@"
// (e.g. documentation with an example hunk header). It requires both a
// "diff --git" header and a properly shaped hunk header -- internal/git's
// GetDiff (ArchGuard's only source of diff text, via `git diff ... --
// path`) always emits both together, so requiring both catches every real
// diff while making a false-positive match on unrelated content very
// unlikely.
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
