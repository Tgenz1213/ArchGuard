package analysis

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
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
		Config:  cfg,
		Content: &MockTruncationProvider{Content: longContent},
	}

	content, mode, err := engine.fetchContext("test.go")
	if err != nil {
		t.Fatalf("fetchContext failed: %v", err)
	}

	if mode != "truncated" {
		t.Errorf("expected mode truncated, got %s", mode)
	}

	t.Logf("Truncated content: %q", content)

	// We expect the content to be rolled back to the newline.
	// MaxTokens=4 typically covers "Line1" + "\n" + "Line" (partial)
	// Smart truncate should yield "Line1\n"
	expected := "Line1\n"
	if content != expected {
		t.Errorf("Expected content to be rolled back to newline (%q), but got %q", expected, content)
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
