package llm

import (
	"testing"
)

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean json",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "markdown wrapped json",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "markdown wrapped json with extra text",
			input:    "Here is the result:\n```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "no block but whitespace",
			input:    "   {\"key\": \"value\"}   ",
			expected: `{"key": "value"}`,
		},
		{
			name:     "triple backticks only",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "chatter before",
			input:    `Here is the JSON you requested: {"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "chatter after",
			input:    `{"key": "value"} Hope this helps!`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "chatter both",
			input:    `Sure! {"key": "value"} Cheers.`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "nested braces",
			input:    `{"key": "value {nested}"}`,
			expected: `{"key": "value {nested}"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanJSON(tt.input)
			if got != tt.expected {
				t.Errorf("CleanJSON() = %q, want %q", got, tt.expected)
			}
		})
	}
}
