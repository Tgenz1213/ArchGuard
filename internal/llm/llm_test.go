package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

func TestSuggestRemediation_ReturnsSuggestionText(t *testing.T) {
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"suggestion": "Move this logic into a Go service and call it from here."}`, nil
		},
	}

	got, err := SuggestRemediation(context.Background(), provider, "adr content", "code content", "file.js", "This file is JavaScript but the ADR mandates Go.", "const x = 1;")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	want := "Move this logic into a Go service and call it from here."
	if got != want {
		t.Errorf("expected suggestion %q, got %q", want, got)
	}
}

func TestSuggestRemediation_PropagatesProviderError(t *testing.T) {
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return "", fmt.Errorf("persistent error")
		},
	}

	_, err := SuggestRemediation(context.Background(), provider, "adr", "code", "file.go", "reasoning", "quoted")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSuggestRemediation_PromptIncludesConfirmedViolationDetails(t *testing.T) {
	var capturedUser, capturedSystem string
	provider := &MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			capturedSystem = system
			capturedUser = user
			return `{"suggestion": "ok"}`, nil
		},
	}

	if _, err := SuggestRemediation(context.Background(), provider, "All services must be Go.", "const x = 1;", "file.js", "This file is JS but ADR mandates Go.", "const x = 1;"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(capturedSystem, "Remediation Advisor") {
		t.Errorf("expected system prompt to identify the Remediation Advisor role, got: %q", capturedSystem)
	}
	if !strings.Contains(capturedUser, "This file is JS but ADR mandates Go.") {
		t.Errorf("expected user prompt to include the confirmed violation's reasoning, got: %q", capturedUser)
	}
	if !strings.Contains(capturedUser, "const x = 1;") {
		t.Errorf("expected user prompt to include the quoted code, got: %q", capturedUser)
	}
}

func TestAnalysisResult_SuggestionOmittedWhenEmpty(t *testing.T) {
	res := AnalysisResult{Violation: false, Reasoning: "no violation", QuotedCode: ""}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if strings.Contains(string(data), "suggestion") {
		t.Errorf("expected suggestion field to be omitted when empty, got: %s", data)
	}
}
