package analysis

import (
	"context"
	"errors"
	"fmt"
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
			if err != nil || diffForEmbedding == "" {
				diffForEmbedding = content
			}

			if len(diffForEmbedding) > 6000 {
				diffForEmbedding = diffForEmbedding[:6000]
			}

			embedding, err := e.Provider.CreateEmbedding(ctx, diffForEmbedding)
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
					header = header[:2000]
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
// byte cutoff from the content's average bytes-per-token ratio (which
// assumes roughly uniform token density across the content), then
// verifies and shrinks that estimate via CountTokens, then rolls back to
// the nearest preceding newline so truncated files don't end mid-line.
//
// The shrink itself happens in two phases so the result is a genuine
// guarantee -- the returned content's token count is always <= maxTokens
// (down to the edge case of an empty result, which is 0 tokens) -- not a
// best-effort bound:
//
//  1. Proportional-shrink estimate: scale the cut by the ratio between the
//     token budget and the actually measured count. This converges in one
//     or two iterations when token density is roughly uniform, but for
//     content whose density is highly non-uniform (e.g. a mostly-ASCII
//     file ending in a dense CJK or base64 block), the whole-content ratio
//     can be a poor local predictor, so this phase is bounded and may not
//     converge.
//  2. Halving fallback: if the proportional phase didn't converge, keep
//     halving the cut and re-measuring until it fits. Integer halving
//     strictly decreases the cut toward 0, so this phase is guaranteed to
//     terminate with a candidate that fits, unlike phase 1.
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
		n, err := e.Provider.CountTokens(ctx, candidate)
		if err != nil {
			return "", err
		}
		if n <= maxTokens {
			break
		}
		cut = clampRuneBoundary(content, cut/2)
		candidate = content[:cut]
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
