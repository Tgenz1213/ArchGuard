package analysis_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/baseline"
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

// TestRun_EmbedsFileContentAsQuery asserts Run embeds with
// EmbeddingTaskQuery, not EmbeddingTaskDocument.
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

// TestRun_UpdateBaselineMode_EmbedsFullContentNotDiff asserts the
// ADR-relevance embedding uses full file content, not a partial diff.
func TestRun_UpdateBaselineMode_EmbedsFullContentNotDiff(t *testing.T) {
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

	diffHunk := "@@ -1,1 +1,1 @@\n-old\n+import python_library\n"
	fullContent := "unrelated preamble\nimport python_library\nmore unrelated content"
	content := &diffCapableContentProvider{content: fullContent, diff: diffHunk}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.UpdateBaseline = true

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if gotText != fullContent {
		t.Errorf("expected update-baseline mode to embed the full file content, got %q, want %q", gotText, fullContent)
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

// diffCapableContentProvider returns distinct content for GetContent and
// GetDiff, so a test can assert which one Run actually used.
type diffCapableContentProvider struct {
	content string
	diff    string
}

func (p *diffCapableContentProvider) GetFiles() ([]string, error) {
	return []string{"service.py"}, nil
}
func (p *diffCapableContentProvider) GetContent(path string) (string, error) { return p.content, nil }
func (p *diffCapableContentProvider) GetDiff(path string) (string, error)    { return p.diff, nil }

// TestRun_NeverStripsFallbackContent asserts stripDiffMetadata never runs
// on whole-file fallback content, even when it looks diff-shaped.
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

// TestRun_UsesEmbedProviderWhenSet asserts Run embeds via EmbedProvider,
// not Provider, when EmbedProvider is set.
func TestRun_UsesEmbedProviderWhenSet(t *testing.T) {
	chatCalled := false
	chatProvider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalled = true
			return `{"violation": false, "reasoning": "none", "quoted_code": ""}`, nil
		},
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			t.Fatal("chatProvider.CreateEmbedding should not be called when EmbedProvider is set")
			return nil, nil
		},
	}

	embedCalled := false
	embedProvider := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			embedCalled = true
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

	engine := analysis.NewEngine(cfg, store, chatProvider, content, false, false)
	engine.Cache = nil
	engine.EmbedProvider = embedProvider
	if err := engine.Run(context.Background()); err != nil && !errors.Is(err, analysis.ErrDriftDetected) {
		t.Fatalf("Run failed: %v", err)
	}

	if !embedCalled {
		t.Error("expected embedProvider.CreateEmbedding to be called")
	}
	if !chatCalled {
		t.Error("expected chatProvider.Chat to be called (ADR similarity search still found the one seeded ADR)")
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

// TestRun_SuppressesBaselinedViolation asserts a violation matching a
// seeded Baseline entry is suppressed rather than surfaced as drift.
func TestRun_SuppressesBaselinedViolation(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
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
		Files: map[string]string{
			"service.py": "import python_library\n// content ignored by mock",
		},
	}

	b := baseline.New()
	b.Add("0001", "service.py", "import python_library")

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.Baseline = b

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error (violation should be suppressed by baseline), got: %v", err)
	}
}

// TestRun_ReSurfacesWhenQuotedCodeNoLongerInFile asserts a Baseline entry
// stops suppressing once its QuotedCode is no longer in the file.
func TestRun_ReSurfacesWhenQuotedCodeNoLongerInFile(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
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
		Files: map[string]string{
			"service.py": "// content ignored by mock, no longer contains the baselined snippet",
		},
	}

	b := baseline.New()
	b.Add("0001", "service.py", "import python_library")

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.Baseline = b

	err := engine.Run(context.Background())
	if err == nil {
		t.Fatal("expected DriftDetectedError, got nil")
	}
	var driftErr *analysis.DriftDetectedError
	if !errors.As(err, &driftErr) {
		t.Fatalf("expected *analysis.DriftDetectedError, got: %v", err)
	}
	if driftErr.Count != 1 {
		t.Errorf("expected Count 1, got %d", driftErr.Count)
	}
}

// TestRun_UpdateBaselineMode_CollectsViolationsAndNeverErrors asserts
// UpdateBaseline never fails on a violation, only records it.
func TestRun_UpdateBaselineMode_CollectsViolationsAndNeverErrors(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
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
		Files: map[string]string{
			"service.py": "import python_library\n// content ignored by mock",
		},
	}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.UpdateBaseline = true

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil {
		t.Fatal("expected CollectedBaseline to be populated")
	}
	if len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected exactly 1 collected entry, got %d", len(engine.CollectedBaseline.Entries))
	}
	got := engine.CollectedBaseline.Entries[0]
	want := baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"}
	if got != want {
		t.Errorf("expected entry %+v, got %+v", want, got)
	}
}

// TestRun_UpdateBaselineMode_IgnoresPreexistingBaselineSuppression asserts
// UpdateBaseline records a violation even if a pre-existing Baseline would suppress it.
func TestRun_UpdateBaselineMode_IgnoresPreexistingBaselineSuppression(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
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
		Files: map[string]string{
			"service.py": "import python_library\n// content ignored by mock",
		},
	}

	b := baseline.New()
	b.Add("0001", "service.py", "import python_library")

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.Baseline = b
	engine.UpdateBaseline = true

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil {
		t.Fatal("expected CollectedBaseline to be populated")
	}
	if len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected the violation to still be recorded despite pre-existing suppression, got %d entries", len(engine.CollectedBaseline.Entries))
	}
	got := engine.CollectedBaseline.Entries[0]
	want := baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"}
	if got != want {
		t.Errorf("expected entry %+v, got %+v", want, got)
	}
}

// TestRun_UpdateBaselineMode_SkipsEntryWhenQuotedCodeNotInFile asserts a
// quoted_code that doesn't match the file verbatim is skipped, not baselined.
func TestRun_UpdateBaselineMode_SkipsEntryWhenQuotedCodeNotInFile(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library_ESCAPED_FORM"
        }`, nil
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
		Files: map[string]string{
			"service.py": "import python_library\n// content ignored by mock",
		},
	}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.UpdateBaseline = true

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil {
		t.Fatal("expected CollectedBaseline to be populated")
	}
	if len(engine.CollectedBaseline.Entries) != 0 {
		t.Fatalf("expected the mismatched entry to be skipped, got %d entries: %+v", len(engine.CollectedBaseline.Entries), engine.CollectedBaseline.Entries)
	}
}

// TestRun_UpdateBaselineMode_CIWarnOpenDoesNotSkipFile asserts CI Warn-Open
// doesn't drop a file from --update-baseline's snapshot.
func TestRun_UpdateBaselineMode_CIWarnOpenDoesNotSkipFile(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
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
		LLM:         config.LLMConfig{MaxTokens: 5}, // small enough that the file below must be truncated
	}
	// Padded well past the token budget, with no diff available, so
	// fetchContext has to truncate it rather than fall back to a diff.
	bigContent := strings.Repeat("x", 200) + "\nimport python_library\n// content ignored by mock"
	content := &fallbackOnlyContentProvider{files: map[string]string{
		"service.py": bigContent,
	}}

	engine := analysis.NewEngine(cfg, store, provider, content, false, true) // ci=true
	engine.Cache = nil
	engine.UpdateBaseline = true

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil {
		t.Fatal("expected CollectedBaseline to be populated")
	}
	if len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected the truncated file to still be analyzed and recorded under --ci --update-baseline, got %d entries", len(engine.CollectedBaseline.Entries))
	}
	got := engine.CollectedBaseline.Entries[0]
	want := baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"}
	if got != want {
		t.Errorf("expected entry %+v, got %+v", want, got)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. Mirrors internal/index's helper of the same name.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read pipe: %v", err)
	}
	return buf.String()
}

// partialErrorContentProvider lets a test deterministically exercise
// Run's per-file fail-open path via a chosen file's GetContent error.
type partialErrorContentProvider struct {
	files    []string
	content  map[string]string
	errFiles map[string]bool
}

func (p *partialErrorContentProvider) GetFiles() ([]string, error) { return p.files, nil }

func (p *partialErrorContentProvider) GetContent(path string) (string, error) {
	if p.errFiles[path] {
		return "", fmt.Errorf("simulated read error for %s", path)
	}
	return p.content[path], nil
}

func (p *partialErrorContentProvider) GetDiff(path string) (string, error) {
	return p.GetContent(path)
}

func TestRun_UpdateBaselineMode_ReportsSkippedFileCount(t *testing.T) {
	provider := &llm.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
		},
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "badembed") {
				return nil, errors.New("simulated embedding failure")
			}
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

	content := &partialErrorContentProvider{
		files: []string{"good.go", "badread.go", "badembed.go"},
		content: map[string]string{
			"good.go":     "import python_library\n// content ignored by mock",
			"badembed.go": "badembed marker content",
		},
		errFiles: map[string]bool{"badread.go": true},
	}

	engine := analysis.NewEngine(cfg, store, provider, content, false, false)
	engine.Cache = nil
	engine.UpdateBaseline = true

	var runErr error
	output := captureStdout(t, func() {
		runErr = engine.Run(context.Background())
	})
	if runErr != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", runErr)
	}

	if engine.CollectedBaseline == nil {
		t.Fatal("expected CollectedBaseline to be populated")
	}
	if len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected exactly 1 collected entry, got %d: %+v", len(engine.CollectedBaseline.Entries), engine.CollectedBaseline.Entries)
	}

	if !strings.Contains(output, "2 file(s) skipped due to errors") {
		t.Fatalf("expected summary to report 2 skipped files, got output: %q", output)
	}
}
