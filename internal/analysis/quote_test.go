package analysis

import "testing"

func TestUnwrapMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		quote string
		want  string
	}{
		{"a fenced block with a language tag", "```go\nx := 1\ny := 2\n```", "x := 1\ny := 2"},
		{"a fenced block without a language tag", "```\nx := 1\n```", "x := 1"},
		{"a single-line fenced block", "```x := 1```", "x := 1"},
		{"surrounding whitespace", "  ```go\nx := 1\n```\n", "x := 1"},
		{"CRLF line endings", "```go\r\nx := 1\r\n```", "x := 1"},
		{"inline backticks", "`x := 1`", "x := 1"},
		{"double inline backticks around a backtick", "`` a`b ``", "a`b"},
		{"no markdown", "x := 1", "x := 1"},
		{"a lone backtick", "`", "`"},
		{"an empty quote", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unwrapMarkdown(tt.quote); got != tt.want {
				t.Fatalf("unwrapMarkdown(%q) = %q, want %q", tt.quote, got, tt.want)
			}
		})
	}
}
