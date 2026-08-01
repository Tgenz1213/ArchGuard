package analysis

import (
	"context"
	"errors"
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
)

type MockTruncationProvider struct {
	Content string
}

func (m *MockTruncationProvider) GetFiles() ([]string, error)            { return []string{"test.go"}, nil }
func (m *MockTruncationProvider) GetContent(path string) (string, error) { return m.Content, nil }
func (m *MockTruncationProvider) GetDiff(path string) (string, error)    { return "", nil }

func TestFetchContext_SmartTruncation(t *testing.T) {
	// A long string with newlines.
	// We want enough tokens so that MaxTokens=5 cuts it off.
	// "Line1" -> ~2 tokens
	// "\n" -> 1 token
	// "Line2" -> ~2 tokens
	// "Line3"
	longContent := "Line1\nLine2\nLine3"

	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: 4,
			Model:     "gpt-3.5-turbo",
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: longContent},
		Provider: llm.NewOpenAIProvider("unused-key", "gpt-3.5-turbo", "unused-embed-model"),
	}

	content, mode, err := engine.fetchContext(context.Background(), "test.go")
	if err != nil {
		t.Fatalf("fetchContext failed: %v", err)
	}

	if mode != "truncated" {
		t.Errorf("expected mode truncated, got %s", mode)
	}

	t.Logf("Truncated content: %q", content)

	// We expect the content to be rolled back to the newline.
	expected := "Line1\n"
	if content != expected {
		t.Errorf("Expected content to be rolled back to newline (%q), but got %q", expected, content)
	}
}

// TestFetchContext_NonOpenAI_UsesProviderTokenCount proves the fix for
// issue #39: truncation for a non-OpenAI provider is driven by that
// provider's own CountTokens, not by tiktoken's cl100k_base fallback.
// The mock provider here counts tokens completely differently from
// tiktoken (1 token per 2 bytes, vs. cl100k_base's real BPE), so if the
// engine were still silently using tiktoken under the hood, this test's
// truncation boundary would land somewhere else and the assertion below
// would fail.
func TestFetchContext_NonOpenAI_UsesProviderTokenCount(t *testing.T) {
	// 20 bytes: "AAAAAAAAAA\nBBBBBBBBB" -> mock counts 1 token per 2 bytes = 10 tokens total.
	content := "AAAAAAAAAA\nBBBBBBBBB"

	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: 5, // half of the mock's 10-token total -> must truncate
			Model:     "llama3.2",
			Provider:  "ollama",
		},
	}

	mockProvider := &llm.MockProvider{
		CountTokensFunc: func(ctx context.Context, text string) (int, error) {
			return len(text) / 2, nil
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: content},
		Provider: mockProvider,
	}

	got, mode, err := engine.fetchContext(context.Background(), "test.go")
	if err != nil {
		t.Fatalf("fetchContext failed: %v", err)
	}
	if mode != "truncated" {
		t.Fatalf("expected mode truncated, got %s", mode)
	}
	// MaxTokens=5 * 2 bytes/token = 10 bytes -> "AAAAAAAAAA" (exactly 10
	// bytes, no newline yet) -> no preceding newline to roll back to, so
	// content is returned as-is at the 10-byte boundary.
	expected := "AAAAAAAAAA"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

// TestFetchContext_CountTokensError_PropagatesLoudly proves AC4: if the
// provider can't produce a token count, fetchContext returns an error
// instead of silently falling back to a length-based heuristic.
func TestFetchContext_CountTokensError_PropagatesLoudly(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: 100,
			Model:     "some-model",
			Provider:  "ollama",
		},
	}

	mockProvider := &llm.MockProvider{
		CountTokensFunc: func(ctx context.Context, text string) (int, error) {
			return 0, errors.New("model not found on server")
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: "some file content"},
		Provider: mockProvider,
	}

	_, _, err := engine.fetchContext(context.Background(), "test.go")
	if err == nil {
		t.Fatal("expected fetchContext to return an error when CountTokens fails, got nil")
	}
}

func TestShouldExclude_RecursiveTestPattern(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.Analysis{
			ExcludePatterns: []string{"**/*_test.go", "vendor/**"},
		},
	}
	engine := &Engine{Config: cfg}

	cases := []struct {
		path string
		want bool
	}{
		{"foo_test.go", true},
		{"internal/analysis/glob_test.go", true}, // regression: previously only matched exactly 2 path segments deep
		{"internal/analysis/glob.go", false},
		{"vendor/pkg/sub/file.go", true},
	}

	for _, c := range cases {
		if got := engine.shouldExclude(c.path); got != c.want {
			t.Errorf("shouldExclude(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
