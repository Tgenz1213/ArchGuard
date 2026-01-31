package analysis

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

// Engine coordinates the analysis of source files against ADRs using LLM providers.
type Engine struct {
	Config   *config.Config
	Store    *index.Store
	Provider llm.Provider
	Content  ContentProvider
	Debug    bool
	CI       bool // CI-safe mode (Warn-Open behavior)
	Cache    *cache.Cache
}

// NewEngine initializes a new analysis engine with a local cache.
func NewEngine(cfg *config.Config, store *index.Store, provider llm.Provider, content ContentProvider, debug bool, ci bool) *Engine {
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
		wg         sync.WaitGroup
	)

	// Worker pool semaphore (concurrency limit provided by config or default 5)
	concurrency := e.Config.Analysis.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	sem := make(chan struct{}, concurrency)

	for _, file := range files {
		if e.shouldExclude(file) {
			continue
		}

		wg.Add(1)
		go func(file string) {
			defer wg.Done()

			// buffer output to ensure atomic printing per file
			var sb strings.Builder

			sem <- struct{}{}
			defer func() { <-sem }()

			if e.Debug {
				fmt.Fprintf(&sb, "Analyzing %s...\n", file)
			}

			content, diffMode, err := e.fetchContext(file)
			if err != nil {
				fmt.Fprintf(&sb, "Error reading file %s: %v\n", file, err)
				mu.Lock()
				fmt.Print(sb.String())
				mu.Unlock()
				return
			}

			if e.Debug {
				fmt.Fprintf(&sb, "  Context mode: %s\n", diffMode)
			}

			if diffMode == "truncated" && e.CI {
				fmt.Fprintf(&sb, "  [WARN-OPEN] File %s was truncated for analysis. In CI mode this is treated as a warning (no failure).\n", file)
				mu.Lock()
				fmt.Print(sb.String())
				mu.Unlock()
				return
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
				return
			}

			hits := e.Store.Search(embedding, e.Config.VectorStore.SimilarityThreshold, 3)
			if len(hits) == 0 {
				if e.Debug {
					fmt.Fprintf(&sb, "  No relevant ADRs found.\n")
				}
				mu.Lock()
				fmt.Print(sb.String())
				mu.Unlock()
				return
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
		}(file)
	}

	wg.Wait()

	if violations > 0 {
		return fmt.Errorf("found %d architectural violations", violations)
	}

	return nil
}

func (e *Engine) shouldExclude(path string) bool {
	for _, pattern := range e.Config.Analysis.ExcludePatterns {
		matched, _ := filepath.Match(pattern, path)
		if matched {
			return true
		}
		if strings.Contains(pattern, "**") {
			prefix := strings.TrimSuffix(pattern, "**")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}
	return false
}

func (e *Engine) fetchContext(path string) (string, string, error) {
	maxTokens := e.Config.LLM.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8000
	}

	fullContent, err := e.Content.GetContent(path)
	if err != nil {
		return "", "", err
	}

	tkm, err := e.getTokenizer()
	if err != nil {
		// Fallback if tokenizer fails completely (unlikely with cl100k_base fallback)
		e.Log("Tokenizer initialization failed: %v", err)
		if len(fullContent) > maxTokens*4 {
			return fullContent[:maxTokens*4], "truncated", nil
		}
		return fullContent, "full", nil
	}

	tokenIds := tkm.Encode(fullContent, nil, nil)
	if len(tokenIds) <= maxTokens {
		return fullContent, "full", nil
	}

	diff, err := e.Content.GetDiff(path)
	if err != nil || diff == "" {
		// Truncate using tokens for precision
		truncatedIds := tokenIds[:maxTokens]
		truncatedContent := tkm.Decode(truncatedIds)

		// Smart Truncate: Roll back to the nearest preceding newline character
		if lastNewline := strings.LastIndex(truncatedContent, "\n"); lastNewline != -1 {
			truncatedContent = truncatedContent[:lastNewline+1]
		}

		return truncatedContent, "truncated", nil
	}
	return diff, "diff", nil
}

func (e *Engine) getTokenizer() (*tiktoken.Tiktoken, error) {
	model := e.Config.LLM.Model
	if model == "" {
		model = "gpt-3.5-turbo"
	}

	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		// Fallback to cl100k_base for unknown models (e.g. Ollama)
		return tiktoken.GetEncoding("cl100k_base")
	}
	return tkm, nil
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
