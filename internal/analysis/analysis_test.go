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

	"github.com/cenkalti/backoff/v4"
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

type MockContentProvider struct {
	Files map[string]string
}

func (m *MockContentProvider) GetFiles(context.Context) ([]string, error) {
	var files []string
	for k := range m.Files {
		files = append(files, k)
	}

	return files, nil
}

func (m *MockContentProvider) GetContent(_ context.Context, path string) (string, error) {
	if content, ok := m.Files[path]; ok {
		return content, nil
	}

	return "", nil
}

func (m *MockContentProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return m.GetContent(ctx, path)
}

func TestDriftDetection(t *testing.T) {
	provider := &inference.MockProvider{
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
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0}, // Force match
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{
			"service.py": "// content ignored by mock",
		},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	err := engine.Run(context.Background())

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

func TestRun_EmbedsFileContentAsQuery(t *testing.T) {
	var gotTask inference.EmbeddingTaskType
	provider := &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)

	engine.Cache = nil
	if err := engine.Run(context.Background()); err != nil && !errors.Is(err, analysis.ErrDriftDetected) {
		t.Fatalf("Run failed: %v", err)
	}

	if gotTask != inference.EmbeddingTaskQuery {
		t.Errorf("expected EmbeddingTaskQuery, got %v", gotTask)
	}
}

func TestRun_UpdateBaselineMode_EmbedsFullContentNotDiff(t *testing.T) {
	var gotText string
	provider := &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			gotText = text
			v := make([]float32, 1536)
			v[0] = 1.0
			return v, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{
		ID:        "0001",
		Title:     "Use Golang",
		Status:    "Accepted",
		Content:   "All services must be Go.",
		Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
	}}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	diffHunk := "@@ -1,1 +1,1 @@\n-old\n+import python_library\n"
	fullContent := "unrelated preamble\nimport python_library\nmore unrelated content"
	content := &diffCapableContentProvider{content: fullContent, diff: diffHunk}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
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

func (p *fallbackOnlyContentProvider) GetFiles(context.Context) ([]string, error) {
	var files []string
	for k := range p.files {
		files = append(files, k)
	}

	return files, nil
}

func (p *fallbackOnlyContentProvider) GetContent(_ context.Context, path string) (string, error) {
	return p.files[path], nil
}

func (p *fallbackOnlyContentProvider) GetDiff(_ context.Context, path string) (string, error) {
	return "", nil
}

// diffCapableContentProvider returns distinct content for GetContent and
// GetDiff, so a test can assert which one Run actually used.
type diffCapableContentProvider struct {
	content string
	diff    string
}

func (p *diffCapableContentProvider) GetFiles(context.Context) ([]string, error) {
	return []string{"service.py"}, nil
}

func (p *diffCapableContentProvider) GetContent(_ context.Context, path string) (string, error) {
	return p.content, nil
}

func (p *diffCapableContentProvider) GetDiff(_ context.Context, path string) (string, error) {
	return p.diff, nil
}

// Fallback content that merely looks like a diff must still be left intact.
func TestRun_NeverStripsFallbackContent(t *testing.T) {
	diffLookalike := "diff --git a/x b/x\nindex 111..222 100644\n--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n real content that must survive untouched\n more real content"

	var gotText string
	provider := &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			gotText = text
			v := make([]float32, 1536)
			v[0] = 1.0
			return v, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{
		ID:        "0001",
		Title:     "Use Golang",
		Status:    "Accepted",
		Content:   "All services must be Go.",
		Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
	}}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &fallbackOnlyContentProvider{files: map[string]string{"docs.md": diffLookalike}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)

	engine.Cache = nil
	if err := engine.Run(context.Background()); err != nil && !errors.Is(err, analysis.ErrDriftDetected) {
		t.Fatalf("Run failed: %v", err)
	}

	if gotText != diffLookalike {
		t.Errorf("fallback content was stripped/altered before embedding.\ngot:  %q\nwant: %q", gotText, diffLookalike)
	}
}

func TestRun_EmbedsWithEmbedNotChat(t *testing.T) {
	chatCalled := false
	chatProvider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalled = true
			return `{"violation": false, "reasoning": "none", "quoted_code": ""}`, nil
		},
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			t.Fatal("chatProvider.CreateEmbedding should not be called: embedding goes through Engine.Embed")
			return nil, nil
		},
	}

	embedCalled := false
	embedProvider := &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
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

	engine := analysis.NewEngine(cfg, store, chatProvider, embedProvider, content)
	engine.Cache = nil

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
	var capturedSystemPrompt, capturedUserPrompt string

	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			capturedSystemPrompt = system
			capturedUserPrompt = user
			return `{"violation": false, "reasoning": "none", "quoted_code": ""}`, nil
		},
	}

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

	cfg := &config.Config{
		LLM: config.LLMConfig{
			SystemPrompt: expectedSystemPrompt,
		},
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{
			"test.go": "package test",
		},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	err := engine.Run(context.Background())

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if capturedSystemPrompt != expectedSystemPrompt {
		t.Errorf("Expected system prompt %q, got %q", expectedSystemPrompt, capturedSystemPrompt)
	}

	for _, leaked := range []string{"LOGICAL STEPS", "literal", "NO INFERENCE", "COMPLIANCE IS NOT A VIOLATION", "### TASK", "Determine whether"} {
		if strings.Contains(capturedUserPrompt, leaked) {
			t.Errorf("user prompt leaked ArchGuard judgment framing %q despite custom system_prompt:\n%s", leaked, capturedUserPrompt)
		}
	}
}

type concurrencyTrackingProvider struct {
	mu      sync.Mutex
	active  int
	maxSeen int
	files   []string
}

func (p *concurrencyTrackingProvider) GetFiles(context.Context) ([]string, error) {
	return p.files, nil
}

func (p *concurrencyTrackingProvider) GetContent(_ context.Context, path string) (string, error) {
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

func (p *concurrencyTrackingProvider) GetDiff(_ context.Context, path string) (string, error) {
	return "", nil
}

func TestRun_RespectsMaxConcurrency(t *testing.T) {
	files := make([]string, 10)
	for i := range files {
		files[i] = fmt.Sprintf("file%d.go", i)
	}

	content := &concurrencyTrackingProvider{files: files}

	provider := &inference.MockProvider{}
	store := index.NewLocalStore(5) // no ADRs -> no LLM calls, exercises the goroutine path cheaply

	cfg := &config.Config{
		Analysis: config.Analysis{MaxConcurrency: 3, ExcludePatterns: []string{}},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
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

func TestRun_SuppressesBaselinedViolation(t *testing.T) {
	provider := &inference.MockProvider{
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
	b.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"})

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.Baseline = b

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error (violation should be suppressed by baseline), got: %v", err)
	}
}

func TestRun_ReSurfacesWhenQuotedCodeNoLongerInFile(t *testing.T) {
	provider := &inference.MockProvider{
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
	b.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"})

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
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

func TestRun_UpdateBaselineMode_CollectsViolationsAndNeverErrors(t *testing.T) {
	provider := &inference.MockProvider{
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
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

func TestRun_UpdateBaselineMode_CarriesForwardPreviousReason(t *testing.T) {
	provider := &inference.MockProvider{
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

	priorBaseline := baseline.New()
	priorBaseline.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library", Reason: "accepted-debt"})

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.UpdateBaseline = true
	engine.Baseline = priorBaseline

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil || len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected exactly 1 collected entry, got %+v", engine.CollectedBaseline)
	}

	got := engine.CollectedBaseline.Entries[0]
	if got.Reason != "accepted-debt" {
		t.Errorf("expected carried-forward Reason %q, got %q", "accepted-debt", got.Reason)
	}
}

func TestRun_UpdateBaselineMode_ExplicitBaselineReasonOverridesCarryForward(t *testing.T) {
	provider := &inference.MockProvider{
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

	priorBaseline := baseline.New()
	priorBaseline.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library", Reason: "accepted-debt"})

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.UpdateBaseline = true
	engine.Baseline = priorBaseline
	engine.BaselineReason = "false-positive"

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil || len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected exactly 1 collected entry, got %+v", engine.CollectedBaseline)
	}

	got := engine.CollectedBaseline.Entries[0]
	if got.Reason != "false-positive" {
		t.Errorf("expected explicit BaselineReason %q to override carry-forward, got %q", "false-positive", got.Reason)
	}
}

func TestRun_UpdateBaselineMode_NoExistingReason_NewEntryHasEmptyReason(t *testing.T) {
	provider := &inference.MockProvider{
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.UpdateBaseline = true

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if engine.CollectedBaseline == nil || len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected exactly 1 collected entry, got %+v", engine.CollectedBaseline)
	}

	got := engine.CollectedBaseline.Entries[0]
	if got.Reason != "" {
		t.Errorf("expected empty Reason for a brand-new entry, got %q", got.Reason)
	}
}

func TestRun_UpdateBaselineMode_IgnoresPreexistingBaselineSuppression(t *testing.T) {
	provider := &inference.MockProvider{
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
	b.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"})

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
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

// A QuotedCode absent from the content is a hallucinated quote: flag it
// visibly instead of printing a fabricated-looking "Line 0".
func TestRun_ViolationOutputFlagsUnverifiedQuotedCode(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "this snippet was never in the file"
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	var runErr error
	output := captureStderr(t, func() {
		runErr = engine.Run(context.Background())
	})

	if !errors.Is(runErr, analysis.ErrDriftDetected) {
		t.Fatalf("expected a drift-detected error, got: %v", runErr)
	}

	if strings.Contains(output, "Line 0") {
		t.Errorf("expected no fabricated Line 0, got: %q", output)
	}

	violations := engine.Report().Violations
	if len(violations) != 1 || violations[0].Verified || violations[0].Code != "this snippet was never in the file" {
		t.Errorf("expected one unverified violation quoting the snippet, got %+v", violations)
	}
}

// The LLM sees content after inference.EscapePromptDelimiter, so a quote containing
// a delimiter such as triple backticks must be verified against the escaped form.
func TestRun_ViolationOutputVerifiesAgainstEscapedContent(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Fenced code block found.",
            "quoted_code": "'''python\nimport python_library\n'''"
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
			"README.md": "```python\nimport python_library\n```\n// content ignored by mock",
		},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	var runErr error
	output := captureStderr(t, func() {
		runErr = engine.Run(context.Background())
	})

	if !errors.Is(runErr, analysis.ErrDriftDetected) {
		t.Fatalf("expected a drift-detected error, got: %v", runErr)
	}

	if strings.Contains(output, "UNVERIFIED") {
		t.Errorf("expected the escaped-form quote to verify, got: %q", output)
	}

	violations := engine.Report().Violations
	if len(violations) != 1 || !violations[0].Verified || violations[0].Line != 1 {
		t.Errorf("expected one verified violation at line 1, got %+v", violations)
	}
}

func TestRun_UpdateBaselineMode_SkipsEntryWhenQuotedCodeNotInFile(t *testing.T) {
	provider := &inference.MockProvider{
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
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

func TestRun_UpdateBaselineMode_CIWarnOpenDoesNotSkipFile(t *testing.T) {
	provider := &inference.MockProvider{
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.CI = true
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

// runEngine fails the test unless Run returns drift exactly when wantDrift is set.
func runEngine(t *testing.T, engine *analysis.Engine, wantDrift bool) {
	t.Helper()

	err := engine.Run(context.Background())
	if wantDrift && !errors.Is(err, analysis.ErrDriftDetected) {
		t.Errorf("Run() = %v, want drift", err)
	}

	if !wantDrift && err != nil {
		t.Errorf("Run() = %v, want nil", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read pipe: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("failed to close pipe reader: %v", err)
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

func (p *partialErrorContentProvider) GetFiles(context.Context) ([]string, error) {
	return p.files, nil
}

func (p *partialErrorContentProvider) GetContent(_ context.Context, path string) (string, error) {
	if p.errFiles[path] {
		return "", fmt.Errorf("simulated read error for %s", path)
	}

	return p.content[path], nil
}

func (p *partialErrorContentProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return p.GetContent(ctx, path)
}

func TestRun_UpdateBaselineMode_ReportsSkippedFileCount(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
		},
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.UpdateBaseline = true

	var runErr error

	output := captureStderr(t, func() {
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

	if len(engine.SkippedFiles) != 2 {
		t.Fatalf("expected SkippedFiles to be 2, got %+v", engine.SkippedFiles)
	}

	for i, want := range []string{"badembed.go", "badread.go"} {
		if got := engine.SkippedFiles[i]; got.File != want || got.Reason == "" {
			t.Errorf("SkippedFiles[%d] = %+v, want file %s with a reason", i, got, want)
		}
	}

	if !strings.Contains(output, "badread.go\n  Error: reading file: ") {
		t.Fatalf("expected per-file error to still be logged, got output: %q", output)
	}
}

func TestRun_ViolationOutputFormat(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{
            "violation": true,
            "reasoning": "Python is not allowed.",
            "quoted_code": "import python_library"
        }`, nil
		},
	}
	newStore := func() *index.LocalStore {
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
		return store
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	newContent := func() *MockContentProvider {
		return &MockContentProvider{
			Files: map[string]string{
				"service.py": "import python_library\n// content ignored by mock",
			},
		}
	}

	t.Run("new violation", func(t *testing.T) {
		engine := analysis.NewEngine(cfg, newStore(), provider, provider, newContent())
		engine.Cache = nil

		var runErr error
		output := captureStderr(t, func() {
			runErr = engine.Run(context.Background())
		})

		if !errors.Is(runErr, analysis.ErrDriftDetected) {
			t.Fatalf("expected a drift-detected error, got: %v", runErr)
		}

		violations := engine.Report().Violations
		if len(violations) != 1 || violations[0].File != "service.py" || violations[0].ADRID != "0001" || violations[0].Line != 1 {
			t.Errorf("expected the violation in the report, got %+v", violations)
		}

		if !strings.Contains(output, "service.py") || !strings.Contains(output, "0001") {
			t.Errorf("expected the log to name the file and ADR, got: %q", output)
		}
	})

	t.Run("baselined", func(t *testing.T) {
		b := baseline.New()
		b.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library", Reason: "accepted-debt"})

		engine := analysis.NewEngine(cfg, newStore(), provider, provider, newContent())
		engine.Cache = nil
		engine.Baseline = b

		var runErr error
		output := captureStderr(t, func() {
			runErr = engine.Run(context.Background())
		})

		if runErr != nil {
			t.Fatalf("expected no error for a fully-baselined violation, got: %v", runErr)
		}

		if report := engine.Report(); report.Baselined != 1 || len(report.Violations) != 0 {
			t.Errorf("expected one baselined hit and no new violations, got %+v", report)
		}

		if !strings.Contains(output, "service.py") || !strings.Contains(output, "0001") {
			t.Errorf("expected the log to name the file and ADR, got: %q", output)
		}
	})

	t.Run("update baseline", func(t *testing.T) {
		engine := analysis.NewEngine(cfg, newStore(), provider, provider, newContent())
		engine.Cache = nil
		engine.UpdateBaseline = true

		var runErr error
		output := captureStderr(t, func() {
			runErr = engine.Run(context.Background())
		})

		if runErr != nil {
			t.Fatalf("expected no error in update-baseline mode, got: %v", runErr)
		}

		want := "  [VIOLATION] Use Golang [Line 1]\n    Reasoning: Python is not allowed.\n    Code: import python_library\n"
		if !strings.Contains(output, want) {
			t.Errorf("expected exact block %q, got: %q", want, output)
		}
	})

	t.Run("update baseline with explicit reason", func(t *testing.T) {
		engine := analysis.NewEngine(cfg, newStore(), provider, provider, newContent())
		engine.Cache = nil
		engine.UpdateBaseline = true
		engine.BaselineReason = "accepted-debt"

		var runErr error
		output := captureStderr(t, func() {
			runErr = engine.Run(context.Background())
		})

		if runErr != nil {
			t.Fatalf("expected no error in update-baseline mode, got: %v", runErr)
		}

		want := "  [VIOLATION] Use Golang [Line 1]\n    Reasoning: Python is not allowed.\n    Code: import python_library\n    Baseline Reason: accepted-debt\n"
		if !strings.Contains(output, want) {
			t.Errorf("expected exact block %q, got: %q", want, output)
		}
	})
}

// A failed ADR check is counted without blocking the file's other ADRs.
func TestRun_UpdateBaselineMode_ReportsSkippedADRCheckCount(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(user, "BADADRMARKER") {
				return "", backoff.Permanent(errors.New("simulated LLM failure"))
			}

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
		{
			ID:        "0002",
			Title:     "Bad ADR",
			Status:    "Accepted",
			Content:   "BADADRMARKER content.",
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

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.UpdateBaseline = true

	if err := engine.Run(t.Context()); err != nil {
		t.Fatalf("expected no error in update-baseline mode, got: %v", err)
	}

	if len(engine.FailedChecks) != 1 {
		t.Fatalf("expected FailedChecks to be 1, got %+v", engine.FailedChecks)
	}

	if got := engine.FailedChecks[0]; got.File != "service.py" || got.ADRID != "0002" || got.Title != "Bad ADR" || !strings.Contains(got.Reason, "simulated LLM failure") {
		t.Errorf("FailedChecks[0] = %+v, want service.py / ADR 0002 / Bad ADR / the LLM error", got)
	}

	if engine.CollectedBaseline == nil || len(engine.CollectedBaseline.Entries) != 1 {
		t.Fatalf("expected exactly 1 collected entry from the successful ADR, got %+v", engine.CollectedBaseline)
	}

	if engine.CollectedBaseline.Entries[0].ADRID != "0001" {
		t.Errorf("expected the surviving entry to be from ADR 0001, got %+v", engine.CollectedBaseline.Entries[0])
	}
}

func TestRun_ReportsSkippedFileCount(t *testing.T) {
	provider := &inference.MockProvider{
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

	content := &partialErrorContentProvider{
		files: []string{"good.go", "badread.go"},
		content: map[string]string{
			"good.go": "import python_library\n// content ignored by mock",
		},
		errFiles: map[string]bool{"badread.go": true},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	var runErr error
	output := captureStderr(t, func() {
		runErr = engine.Run(context.Background())
	})

	if !errors.Is(runErr, analysis.ErrDriftDetected) {
		t.Fatalf("expected a drift-detected error from the one real violation, got: %v", runErr)
	}

	if skipped := engine.Report().SkippedFiles; len(skipped) != 1 || skipped[0].File != "badread.go" {
		t.Fatalf("expected the report to list badread.go as skipped, got %+v (log: %q)", skipped, output)
	}
}

// Skipped ADR checks must be reported even when there are no violations.
func TestRun_ReportsSkippedADRCheckCount(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return "", backoff.Permanent(fmt.Errorf("mock LLM failure"))
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
		files:   []string{"good.go"},
		content: map[string]string{"good.go": "package good"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	var runErr error
	output := captureStderr(t, func() {
		runErr = engine.Run(t.Context())
	})

	if runErr != nil {
		t.Fatalf("expected no error (zero violations), got: %v", runErr)
	}

	if len(engine.FailedChecks) != 1 {
		t.Fatalf("expected FailedChecks to be 1, got %+v", engine.FailedChecks)
	}

	if failed := engine.Report().FailedChecks; len(failed) != 1 || failed[0].File != "good.go" {
		t.Fatalf("expected the report to list the failed ADR check on good.go, got %+v (log: %q)", failed, output)
	}
}

// proves Engine.Run's file path reaches Store.Search's scope filter: same
// embedding for both files, so only scope explains the differing outcome.
func TestRun_ScopeRestrictedADROnlyEvaluatedForMatchingFile(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": true, "reasoning": "always violates in this test", "quoted_code": "bad"}`, nil
		},
	}

	store := index.NewLocalStore(1)
	store.ADRs = []index.ADR{
		{
			ID:        "0001",
			Title:     "Go-only rule",
			Status:    "Accepted",
			Scope:     index.ScopePatterns{"**/*.go"},
			Content:   "Go files must do X.",
			Embedding: func() []float32 { v := make([]float32, 4); v[0] = 1.0; return v }(),
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{
			"service.go": "package main",
			"service.rb": "puts 'hi'",
		},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	err := engine.Run(context.Background())

	// Only service.go's ADR check should fire and produce a violation;
	// service.rb's scope mismatch means the ADR is never evaluated for it.
	var driftErr *analysis.DriftDetectedError
	if !errors.As(err, &driftErr) {
		t.Fatalf("expected a DriftDetectedError, got %v", err)
	}

	if driftErr.Count != 1 {
		t.Errorf("expected exactly 1 violation (from service.go only), got %d", driftErr.Count)
	}
}

func TestRun_DebugMode_LogsBelowThresholdADRScore(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:        "0002",
			Title:     "Near Miss ADR",
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.9},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		runEngine(t, engine, false)
	})

	if !strings.Contains(output, "Below threshold: Near Miss ADR (score 0.71 < threshold 0.90)") {
		t.Fatalf("expected the below-threshold debug line with title and score, got: %q", output)
	}
}

func TestRun_DebugMode_LogsTopKTruncatedADRs(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	makeEmbedding := func(x, y float32) []float32 {
		v := make([]float32, 1536)
		v[0] = x
		v[1] = y
		return v
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "First ADR", Status: "Accepted", Content: "Rule one.", Embedding: makeEmbedding(1, 0)},
		{ID: "0002", Title: "Second ADR", Status: "Accepted", Content: "Rule two.", Embedding: makeEmbedding(0.9, 0.1)},
		{ID: "0003", Title: "Third ADR", Status: "Accepted", Content: "Rule three.", Embedding: makeEmbedding(0.8, 0.2)},
		{ID: "0004", Title: "Fourth ADR", Status: "Accepted", Content: "Rule four.", Embedding: makeEmbedding(0.7, 0.3)},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.1},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		runEngine(t, engine, false)
	})

	if !strings.Contains(output, "Cut by top-K limit: Fourth ADR") {
		t.Fatalf("expected a top-K-truncated debug line naming the 4th-ranked ADR, got: %q", output)
	}

	if !strings.Contains(output, "rank 4 of 4 qualifying ADRs") {
		t.Fatalf("expected the truncated line to report rank 4 of 4, got: %q", output)
	}

	if strings.Contains(output, "Cut by top-K limit: First ADR") ||
		strings.Contains(output, "Cut by top-K limit: Second ADR") ||
		strings.Contains(output, "Cut by top-K limit: Third ADR") {
		t.Fatalf("only the ADR(s) beyond topK=3 should be reported as truncated, got: %q", output)
	}
}

func TestRun_DebugMode_NoTopKTruncatedLineWhenFewerThanTopKQualify(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:        "0001",
			Title:     "Only ADR",
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1; return v }(),
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.1},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		runEngine(t, engine, false)
	})

	if strings.Contains(output, "Cut by top-K limit") {
		t.Fatalf("expected no top-K-truncated line when fewer than topK ADRs qualify, got: %q", output)
	}
}

// countingTruncatedStore wraps a VectorStore to record how many times
// SearchTruncated is called, so non-debug runs can be proven not to pay for it.
type countingTruncatedStore struct {
	index.VectorStore
	searchTruncatedCalls int
}

func (c *countingTruncatedStore) SearchTruncated(queryEmbedding []float32, threshold float64, topK int, filePath string) []index.SearchResult {
	c.searchTruncatedCalls++
	return c.VectorStore.SearchTruncated(queryEmbedding, threshold, topK, filePath)
}

func TestRun_NonDebugMode_NeverCallsSearchTruncated(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	base := index.NewLocalStore(5)
	base.ADRs = []index.ADR{
		{
			ID:        "0002",
			Title:     "Near Miss ADR",
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}
	store := &countingTruncatedStore{VectorStore: base}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.9},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.searchTruncatedCalls != 0 {
		t.Fatalf("expected SearchTruncated to never be called outside debug mode, got %d calls", store.searchTruncatedCalls)
	}
}

// countingStore wraps a VectorStore to record how many times SearchRejected
// is called, so non-debug runs can be proven not to pay for it.
type countingStore struct {
	index.VectorStore
	searchRejectedCalls int
}

func (c *countingStore) SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []index.SearchResult {
	c.searchRejectedCalls++
	return c.VectorStore.SearchRejected(queryEmbedding, threshold, topK, filePath)
}

func TestRun_NonDebugMode_NeverCallsSearchRejected(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	base := index.NewLocalStore(5)
	base.ADRs = []index.ADR{
		{
			ID:        "0002",
			Title:     "Near Miss ADR",
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}
	store := &countingStore{VectorStore: base}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.9},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.searchRejectedCalls != 0 {
		t.Fatalf("expected SearchRejected to never be called outside debug mode, got %d calls", store.searchRejectedCalls)
	}
}

// countingDebugInfoStore counts each Search* call, to prove debug runs make one
// SearchWithDebugInfo query rather than three that could disagree.
type countingDebugInfoStore struct {
	index.VectorStore
	searchCalls              int
	searchRejectedCalls      int
	searchTruncatedCalls     int
	searchWithDebugInfoCalls int
}

func (c *countingDebugInfoStore) Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []index.SearchResult {
	c.searchCalls++
	return c.VectorStore.Search(queryEmbedding, threshold, topK, filePath)
}

func (c *countingDebugInfoStore) SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []index.SearchResult {
	c.searchRejectedCalls++
	return c.VectorStore.SearchRejected(queryEmbedding, threshold, topK, filePath)
}

func (c *countingDebugInfoStore) SearchTruncated(queryEmbedding []float32, threshold float64, topK int, filePath string) []index.SearchResult {
	c.searchTruncatedCalls++
	return c.VectorStore.SearchTruncated(queryEmbedding, threshold, topK, filePath)
}

func (c *countingDebugInfoStore) SearchWithDebugInfo(queryEmbedding []float32, threshold float64, topK int, filePath string) (hits, rejected, truncated []index.SearchResult) {
	c.searchWithDebugInfoCalls++
	return c.VectorStore.SearchWithDebugInfo(queryEmbedding, threshold, topK, filePath)
}

func TestRun_DebugMode_UsesSingleConsolidatedQueryNotThreeIndependentOnes(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	base := index.NewLocalStore(5)
	base.ADRs = []index.ADR{
		{
			ID:        "0002",
			Title:     "Near Miss ADR",
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}
	store := &countingDebugInfoStore{VectorStore: base}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.9},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.searchWithDebugInfoCalls != 1 {
		t.Fatalf("expected SearchWithDebugInfo to be called exactly once in debug mode, got %d calls", store.searchWithDebugInfoCalls)
	}

	if store.searchCalls != 0 || store.searchRejectedCalls != 0 || store.searchTruncatedCalls != 0 {
		t.Fatalf("expected debug mode to derive hits/rejected/truncated from the single SearchWithDebugInfo call, not independent Search/SearchRejected/SearchTruncated calls (got Search=%d, SearchRejected=%d, SearchTruncated=%d)",
			store.searchCalls, store.searchRejectedCalls, store.searchTruncatedCalls)
	}
}

func TestRun_NonDebugMode_NeverCallsSearchWithDebugInfo(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	base := index.NewLocalStore(5)
	base.ADRs = []index.ADR{
		{
			ID:        "0002",
			Title:     "Near Miss ADR",
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}
	store := &countingDebugInfoStore{VectorStore: base}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.9},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.searchWithDebugInfoCalls != 0 {
		t.Fatalf("expected SearchWithDebugInfo to never be called outside debug mode, got %d calls", store.searchWithDebugInfoCalls)
	}

	if store.searchCalls != 1 {
		t.Fatalf("expected exactly one plain Search call outside debug mode, got %d", store.searchCalls)
	}
}

func TestRun_ADRSimilarityThresholdOverride_LowersEffectiveThreshold(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": true, "reasoning": "matched", "quoted_code": "package main"}`, nil
		},
	}

	lenientThreshold := 0.5
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:                  "0011",
			Title:               "Lenient Override ADR",
			Status:              "Accepted",
			Content:             "Some rule.",
			SimilarityThreshold: &lenientThreshold,
			Embedding:           func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.9},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	err := engine.Run(context.Background())

	var driftErr *analysis.DriftDetectedError
	if !errors.As(err, &driftErr) {
		t.Fatalf("expected a DriftDetectedError: the ADR's own 0.5 threshold should admit the ~0.71-similarity match the global 0.9 would reject, got %v", err)
	}

	if driftErr.Count != 1 {
		t.Errorf("expected exactly 1 violation, got %d", driftErr.Count)
	}
}

func TestRun_DebugMode_LogsBelowThresholdADRScore_UsesPerADROverride(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	strictThreshold := 0.95
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{
			ID:                  "0012",
			Title:               "Strict Override ADR",
			Status:              "Accepted",
			Content:             "Some rule.",
			SimilarityThreshold: &strictThreshold,
			Embedding:           func() []float32 { v := make([]float32, 1536); v[0] = 0.7; v[1] = 0.7; return v }(),
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.5},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"service.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		runEngine(t, engine, false)
	})

	if !strings.Contains(output, "Below threshold: Strict Override ADR (score 0.71 < threshold 0.95)") {
		t.Fatalf("expected the debug line to print the ADR's own override (0.95), not the global 0.50, got: %q", output)
	}
}

func TestRun_DebugMode_LogsExplicitlyRequestedFileExcluded(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			t.Fatal("LLM should not be called for an excluded file")
			return "", nil
		},
	}

	cfg := &config.Config{
		Analysis: config.Analysis{ExcludePatterns: []string{"**/*.pb.go"}},
	}

	content := &analysis.MultiFileProvider{Paths: []string{"generated.pb.go"}}

	engine := analysis.NewEngine(cfg, index.NewLocalStore(5), provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		if err := engine.Run(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(output, "Skipping generated.pb.go: explicitly requested but matches exclude_patterns") {
		t.Fatalf("expected a debug line naming the excluded explicit file, got: %q", output)
	}
}

// The baseline file is excluded regardless of exclude_patterns, so its
// skip message must not blame exclude_patterns.
func TestRun_DebugMode_ExplicitlyRequestedBaselineFile_NoExcludePatternsMessage(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			t.Fatal("LLM should not be called for an excluded file")
			return "", nil
		},
	}

	cfg := &config.Config{
		Analysis: config.Analysis{ExcludePatterns: []string{}},
	}

	content := &analysis.MultiFileProvider{Paths: []string{baseline.Path}}

	engine := analysis.NewEngine(cfg, index.NewLocalStore(5), provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		if err := engine.Run(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if strings.Contains(output, "matches exclude_patterns") {
		t.Fatalf("expected no exclude_patterns message for the always-excluded baseline file, got: %q", output)
	}
}

// Only a file the user named explicitly gets a --debug skip message;
// broad scans stay silent about excluded files.
func TestRun_DebugMode_NonExplicitProviderExcludedFile_NoSkipMessage(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			t.Fatal("LLM should not be called for an excluded file")
			return "", nil
		},
	}

	cfg := &config.Config{
		Analysis: config.Analysis{ExcludePatterns: []string{"**/*.pb.go"}},
	}

	content := &MockContentProvider{
		Files: map[string]string{"generated.pb.go": "package main"},
	}

	engine := analysis.NewEngine(cfg, index.NewLocalStore(5), provider, provider, content)
	engine.Debug = true
	engine.Cache = nil

	output := captureStderr(t, func() {
		if err := engine.Run(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if strings.Contains(output, "Skipping") || strings.Contains(output, "matches exclude_patterns") {
		t.Fatalf("expected no skip message for a non-explicit (non-MultiFileProvider) scan, got: %q", output)
	}
}

func TestRun_NonDebugMode_SilentForExplicitlyRequestedExcludedFile(t *testing.T) {
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			t.Fatal("LLM should not be called for an excluded file")
			return "", nil
		},
	}

	cfg := &config.Config{
		Analysis: config.Analysis{ExcludePatterns: []string{"**/*.pb.go"}},
	}

	content := &analysis.MultiFileProvider{Paths: []string{"generated.pb.go"}}

	engine := analysis.NewEngine(cfg, index.NewLocalStore(5), provider, provider, content)
	engine.Cache = nil

	output := captureStderr(t, func() {
		if err := engine.Run(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if output != "" {
		t.Fatalf("expected no output in non-debug mode for an excluded file, got: %q", output)
	}
}

func TestRun_SuggestFixesDisabled_NoExtraCallNoSuggestionOutput(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalls++
			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	// engine.SuggestFixes left at its zero value (false) -- this is the default-off assertion.

	output := captureStderr(t, func() {
		runEngine(t, engine, true)
	})

	if chatCalls != 1 {
		t.Errorf("expected exactly 1 chat call (no suggestion call) when SuggestFixes is off, got %d", chatCalls)
	}

	if strings.Contains(output, "Suggestion") {
		t.Errorf("expected no Suggestion line in output when SuggestFixes is off, got: %s", output)
	}
}

func TestRun_SuggestFixesEnabled_AddsSuggestionLineAndJSONField(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalls++

			if strings.Contains(system, "Remediation Advisor") {
				return `{"suggestion": "Rewrite this in Go, not Python."}`, nil
			}

			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.SuggestFixes = true
	engine.JSONOutput = true

	_ = captureStderr(t, func() {
		runEngine(t, engine, true)
	})

	if chatCalls != 2 {
		t.Fatalf("expected 2 chat calls (violation judgment + suggestion), got %d", chatCalls)
	}

	if len(engine.CollectedViolations) != 1 {
		t.Fatalf("expected 1 collected violation, got %d", len(engine.CollectedViolations))
	}

	if got := engine.CollectedViolations[0].Suggestion; got != "Rewrite this in Go, not Python." {
		t.Errorf("expected Violation.Suggestion to be populated, got %q", got)
	}
}

func TestRun_SuggestFixesEnabled_NoViolation_NeverCallsSuggestion(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalls++
			return `{"violation": false, "reasoning": "no violation", "quoted_code": ""}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "print('ok')\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.SuggestFixes = true

	_ = captureStderr(t, func() {
		runEngine(t, engine, false)
	})

	if chatCalls != 1 {
		t.Errorf("expected exactly 1 chat call when there is no violation, got %d", chatCalls)
	}
}

func TestRun_SuggestFixesEnabled_BaselinedViolation_NoSuggestionCall(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalls++
			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	b := baseline.New()
	b.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: "import python_library"})

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.SuggestFixes = true
	engine.Baseline = b

	output := captureStderr(t, func() {
		runEngine(t, engine, false)
	})

	if chatCalls != 1 {
		t.Errorf("expected exactly 1 chat call for an already-baselined violation, got %d", chatCalls)
	}

	if strings.Contains(output, "Suggestion") {
		t.Errorf("expected no Suggestion line for a baselined violation, got: %s", output)
	}
}

func TestRun_SuggestFixesDisabled_DoesNotSurfaceCachedSuggestionFromPriorFlaggedRun(t *testing.T) {
	c, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "Remediation Advisor") {
				return `{"suggestion": "Rewrite this in Go, not Python."}`, nil
			}

			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	firstEngine := analysis.NewEngine(cfg, store, provider, provider, content)
	firstEngine.Cache = c
	firstEngine.SuggestFixes = true

	_ = captureStderr(t, func() {
		runEngine(t, firstEngine, true)
	})

	if violations := firstEngine.Report().Violations; len(violations) != 1 || violations[0].Suggestion != "Rewrite this in Go, not Python." {
		t.Fatalf("expected first (flagged) run to surface a suggestion, got %+v", violations)
	}

	secondEngine := analysis.NewEngine(cfg, store, provider, provider, content)
	secondEngine.Cache = c
	secondEngine.SuggestFixes = false
	secondEngine.JSONOutput = true

	secondOutput := captureStderr(t, func() {
		runEngine(t, secondEngine, true)
	})
	if strings.Contains(secondOutput, "Suggestion") {
		t.Errorf("expected no Suggestion line when SuggestFixes is off, even with a warm cache, got: %s", secondOutput)
	}

	if len(secondEngine.CollectedViolations) != 1 {
		t.Fatalf("expected 1 collected violation, got %d", len(secondEngine.CollectedViolations))
	}

	if got := secondEngine.CollectedViolations[0].Suggestion; got != "" {
		t.Errorf("expected empty Violation.Suggestion when SuggestFixes is off, got %q", got)
	}
}

func TestRun_SuggestFixesEnabled_StaleSuggestionKeyIsIgnored(t *testing.T) {
	c, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	// Seeds a suggestion under a key computed with different prompt text,
	// simulating a suggestion cached before a suggestion-prompt edit.
	staleKey := cache.ComputeSuggestionKey(cache.SuggestionKeyInput{
		ADRContent:               "All services must be Go.",
		FileContent:              "import python_library\n",
		Filename:                 "service.py",
		Reasoning:                "Python is not allowed.",
		QuotedCode:               "import python_library",
		SuggestionSystemPrompt:   "an old suggestion system prompt",
		SuggestionPromptTemplate: "an old suggestion template",
	})
	if err := c.PutSuggestion(staleKey, "OLD STALE SUGGESTION"); err != nil {
		t.Fatalf("PutSuggestion failed: %v", err)
	}

	suggestionCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "Remediation Advisor") {
				suggestionCalls++
				return `{"suggestion": "NEW FRESH SUGGESTION"}`, nil
			}

			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = c
	engine.SuggestFixes = true

	_ = captureStderr(t, func() {
		runEngine(t, engine, true)
	})

	if suggestionCalls != 1 {
		t.Errorf("expected a fresh suggestion call when the cached entry's key doesn't match, got %d calls", suggestionCalls)
	}

	violations := engine.Report().Violations
	if len(violations) != 1 || violations[0].Suggestion != "NEW FRESH SUGGESTION" {
		t.Errorf("expected only the fresh suggestion in the report, got %+v", violations)
	}
}

func TestRun_SuggestFixesEnabled_UnrelatedEngineChangeReusesCachedSuggestion(t *testing.T) {
	c, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	suggestionCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "Remediation Advisor") {
				suggestionCalls++
				return `{"suggestion": "Rewrite this in Go, not Python."}`, nil
			}

			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	firstEngine := analysis.NewEngine(cfg, store, provider, provider, content)
	firstEngine.Cache = c
	firstEngine.SuggestFixes = true
	_ = captureStderr(t, func() { runEngine(t, firstEngine, true) })

	// Debug toggles between runs but isn't part of the suggestion key, so
	// the cached suggestion should still be reused.
	secondEngine := analysis.NewEngine(cfg, store, provider, provider, content)
	secondEngine.Debug = true
	secondEngine.Cache = c
	secondEngine.SuggestFixes = true
	_ = captureStderr(t, func() { runEngine(t, secondEngine, true) })

	if suggestionCalls != 1 {
		t.Errorf("expected the cached suggestion to be reused (1 total suggestion call across both runs), got %d", suggestionCalls)
	}

	if violations := secondEngine.Report().Violations; len(violations) != 1 || violations[0].Suggestion != "Rewrite this in Go, not Python." {
		t.Errorf("expected the cached suggestion in the second run's report, got %+v", violations)
	}
}

func TestRun_SuggestFixesEnabled_IdenticalContentDifferentFile_GetsIndependentSuggestion(t *testing.T) {
	c, err := cache.NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("cache.NewCache failed: %v", err)
	}

	var mu sync.Mutex
	suggestionCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "Remediation Advisor") {
				mu.Lock()
				suggestionCalls++
				mu.Unlock()

				if strings.Contains(user, "File Path: a/service.py") {
					return `{"suggestion": "Suggestion for a/service.py"}`, nil
				}

				return `{"suggestion": "Suggestion for b/service.py"}`, nil
			}

			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	// Same content under two different paths -- the rendered suggestion
	// prompt differs by File Path, so each must get its own suggestion.
	content := &MockContentProvider{Files: map[string]string{
		"a/service.py": "import python_library\n",
		"b/service.py": "import python_library\n",
	}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = c
	engine.SuggestFixes = true
	_ = captureStderr(t, func() { runEngine(t, engine, true) })

	mu.Lock()
	calls := suggestionCalls
	mu.Unlock()

	if calls != 2 {
		t.Errorf("expected 2 independent suggestion calls for identical content under different paths, got %d", calls)
	}

	suggestions := map[string]string{}
	for _, violation := range engine.Report().Violations {
		suggestions[violation.File] = violation.Suggestion
	}

	if suggestions["a/service.py"] != "Suggestion for a/service.py" || suggestions["b/service.py"] != "Suggestion for b/service.py" {
		t.Errorf("expected each file to surface its own path-specific suggestion, got %+v", suggestions)
	}
}

func TestRun_SuggestFixesEnabled_UnverifiedViolation_NeverCallsSuggestion(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			chatCalls++
			return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "this snippet was never in the file"}`, nil
		},
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{
		{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }()},
	}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.SuggestFixes = true

	output := captureStderr(t, func() {
		runEngine(t, engine, true)
	})

	if chatCalls != 1 {
		t.Errorf("expected exactly 1 chat call (no suggestion call) for an unverified violation, got %d", chatCalls)
	}

	if strings.Contains(output, "Suggestion") {
		t.Errorf("expected no Suggestion line for an unverified violation, got: %s", output)
	}
}

// fourEquallyRelevantADRs builds a store where 4 ADRs equally pass scope
// and threshold, so only topK distinguishes how many reach the LLM.
func fourEquallyRelevantADRs() *index.LocalStore {
	store := index.NewLocalStore(5)
	for i := 0; i < 4; i++ {
		store.ADRs = append(store.ADRs, index.ADR{
			ID:        fmt.Sprintf("%04d", i),
			Title:     fmt.Sprintf("ADR %d", i),
			Status:    "Accepted",
			Content:   "Some rule.",
			Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
		})
	}

	return store
}

func TestRun_MaxRelevantADRs_RaisesLimitAboveDefault(t *testing.T) {
	var mu sync.Mutex
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			mu.Lock()
			chatCalls++
			mu.Unlock()
			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
	}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}, MaxRelevantADRs: 4},
	}
	content := &MockContentProvider{Files: map[string]string{"service.go": "package main"}}

	engine := analysis.NewEngine(cfg, fourEquallyRelevantADRs(), provider, provider, content)
	engine.Cache = nil

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if chatCalls != 4 {
		t.Errorf("expected all 4 qualifying ADRs to reach the LLM with max_relevant_adrs=4, got %d chat calls", chatCalls)
	}
}

func TestRun_MaxRelevantADRs_DefaultsToThreeWhenUnsetOrNonPositive(t *testing.T) {
	for _, maxRelevantADRs := range []int{0, -1} {
		t.Run(fmt.Sprintf("value=%d", maxRelevantADRs), func(t *testing.T) {
			var mu sync.Mutex
			chatCalls := 0
			provider := &inference.MockProvider{
				ChatFunc: func(ctx context.Context, system, user string) (string, error) {
					mu.Lock()
					chatCalls++
					mu.Unlock()
					return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
				},
			}

			cfg := &config.Config{
				VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
				Analysis:    config.Analysis{ExcludePatterns: []string{}, MaxRelevantADRs: maxRelevantADRs},
			}
			content := &MockContentProvider{Files: map[string]string{"service.go": "package main"}}

			engine := analysis.NewEngine(cfg, fourEquallyRelevantADRs(), provider, provider, content)
			engine.Cache = nil

			if err := engine.Run(context.Background()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if chatCalls != 3 {
				t.Errorf("expected the default topK of 3 to apply, got %d chat calls", chatCalls)
			}
		})
	}
}
