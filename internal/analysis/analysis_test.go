package analysis_test

import (
	"context"
	"testing"

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
	store := index.NewStore()
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
	store := index.NewStore()
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
