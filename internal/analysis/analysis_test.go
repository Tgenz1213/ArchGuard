package analysis_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

// MockContentProvider for testing
type MockContentProvider struct {
	Files map[string]string
}

func (m *MockContentProvider) GetFiles() ([]string, error) {
	var files []string
	for k := range m.Files {
		files = append(files, k)
	}
	return files, nil
}

func (m *MockContentProvider) GetContent(path string) (string, error) {
	if content, ok := m.Files[path]; ok {
		return content, nil
	}
	return "", nil
}

func (m *MockContentProvider) GetDiff(path string) (string, error) {
	// For testing, just return content as diff
	return m.GetContent(path)
}

func TestDriftDetection(t *testing.T) {
	// 1. Setup Mock Provider
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			// We simulate the LLM returning a JSON violation
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
		},
	}

	// 2. Setup Store with one ADR
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:        "0001",
			Title:     "Use Golang",
			Status:    "Accepted",
			Content:   "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
		},
	}

	// 3. Setup Config
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0}, // Force match
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	// 4. Setup Mock Content
	content := &MockContentProvider{
		Files: map[string]string{
			"service.py": "// content ignored by mock",
		},
	}

	// 5. Run Engine
	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil // Disable cache for testing
	err := engine.Run(context.Background())

	// 6. Verify Results
	// Expect failure due to violation
	if err == nil {
		t.Fatal("Expected violation error, got nil")
	}
	if err.Error() != "found 1 architectural violations" {
		t.Fatalf("Expected 'found 1 architectural violations', got '%v'", err)
	}
	if !errors.Is(err, analysis.ErrDriftDetected) {
		t.Fatalf("Expected error to match ErrDriftDetected, got '%v'", err)
	}
}

// TestRun_EmbedsFileContentAsQuery asserts Run embeds the file's
// diff/code content with EmbeddingTaskQuery, so a provider capable of
// asymmetric retrieval (e.g. Gemini) searches with it rather than indexing
// it as a document.
func TestRun_EmbedsFileContentAsQuery(t *testing.T) {
	var gotTask llm.EmbeddingTaskType
	provider := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			gotTask = task
			v := make([]float32, 1536)
			v[0] = 1.0
			return v, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:        "0001",
			Title:     "Use Golang",
			Status:    "Accepted",
			Content:   "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{
		Files: map[string]string{"service.py": "// content ignored by mock"},
	}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	if err := engine.Run(context.Background()); err != nil && !errors.Is(err, analysis.ErrDriftDetected) {
		t.Fatalf("Run failed: %v", err)
	}

	if gotTask != llm.EmbeddingTaskQuery {
		t.Errorf("expected EmbeddingTaskQuery, got %v", gotTask)
	}
}

// fallbackOnlyContentProvider always reports no diff, forcing Run onto the
// whole-file-content fallback path regardless of what GetContent returns.
type fallbackOnlyContentProvider struct {
	files map[string]string
}

func (p *fallbackOnlyContentProvider) GetFiles() ([]string, error) {
	var files []string
	for k := range p.files {
		files = append(files, k)
	}
	return files, nil
}

func (p *fallbackOnlyContentProvider) GetContent(path string) (string, error) {
	return p.files[path], nil
}

func (p *fallbackOnlyContentProvider) GetDiff(path string) (string, error) {
	return "", nil
}

// TestRun_NeverStripsFallbackContent asserts Run only ever runs
// stripDiffMetadata on text that actually came from GetDiff, never on the
// whole-file-content fallback -- even when that fallback content happens to
// look diff-shaped (e.g. a doc file with an example diff transcript).
// Stripping fallback content on a heuristic match would corrupt real file
// content that only coincidentally resembles a diff.
func TestRun_NeverStripsFallbackContent(t *testing.T) {
	diffLookalike := "diff --git a/x b/x\nindex 111..222 100644\n--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n real content that must survive untouched\n more real content"

	var gotText string
	provider := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			gotText = text
			v := make([]float32, 1536)
			v[0] = 1.0
			return v, nil
		},
	}

	store := index.NewLocalStore(5)
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &fallbackOnlyContentProvider{files: map[string]string{"docs.md": diffLookalike}}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	if err := engine.Run(context.Background()); err != nil && !errors.Is(err, analysis.ErrDriftDetected) {
		t.Fatalf("Run failed: %v", err)
	}

	if gotText != diffLookalike {
		t.Errorf("fallback content was stripped/altered before embedding.\ngot:  %q\nwant: %q", gotText, diffLookalike)
	}
}

func TestCustomSystemPrompt(t *testing.T) {
	expectedSystemPrompt := "You are a custom system prompt."
	var capturedSystemPrompt string

	// 1. Setup Mock Provider
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			capturedSystemPrompt = system
			return `{"violation": false, "reasoning": "none", "quoted_code": ""}`, nil
		},
	}

	// 2. Setup Store with one ADR
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:        "0001",
			Title:     "Test ADR",
			Status:    "Accepted",
			Content:   "Test content",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
		},
	}

	// 3. Setup Config with custom system prompt
	cfg := &config.Config{
		LLM: config.LLMConfig{
			SystemPrompt: expectedSystemPrompt,
		},
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	// 4. Setup Mock Content
	content := &MockContentProvider{
		Files: map[string]string{
			"test.go": "package test",
		},
	}

	// 5. Run Engine
	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil // Disable cache for testing
	err := engine.Run(context.Background())

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// 6. Verify captured system prompt
	if capturedSystemPrompt != expectedSystemPrompt {
		t.Errorf("Expected system prompt %q, got %q", expectedSystemPrompt, capturedSystemPrompt)
	}
}

type concurrencyTrackingProvider struct {
	mu      sync.Mutex
	active  int
	maxSeen int
	files   []string
}

func (p *concurrencyTrackingProvider) GetFiles() ([]string, error) { return p.files, nil }
func (p *concurrencyTrackingProvider) GetContent(path string) (string, error) {
	p.mu.Lock()
	p.active++
	if p.active > p.maxSeen {
		p.maxSeen = p.active
	}
	p.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	p.mu.Lock()
	p.active--
	p.mu.Unlock()
	return "package main", nil
}
func (p *concurrencyTrackingProvider) GetDiff(path string) (string, error) { return "", nil }

func TestRun_RespectsMaxConcurrency(t *testing.T) {
	files := make([]string, 10)
	for i := range files {
		files[i] = fmt.Sprintf("file%d.go", i)
	}
	content := &concurrencyTrackingProvider{files: files}

	provider := &llm.MockProvider{}
	store := index.NewLocalStore(5) // no ADRs -> no LLM calls, exercises the goroutine path cheaply

	cfg := &config.Config{
		Analysis: config.Analysis{MaxConcurrency: 3, ExcludePatterns: []string{}},
	}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content.mu.Lock()
	defer content.mu.Unlock()
	if content.maxSeen > 3 {
		t.Errorf("expected at most 3 concurrent GetContent calls, saw %d", content.maxSeen)
	}
}
