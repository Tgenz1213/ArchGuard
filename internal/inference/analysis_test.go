package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAnalyzeDrift_DiffInputUsesTheDiffTemplateAndKeepsACustomSystemPrompt(t *testing.T) {
	var gotSystem, gotUser string

	chat := &MockProvider{ChatFunc: func(_ context.Context, system, user string) (string, error) {
		gotSystem, gotUser = system, user

		return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
	}}

	in := DriftInput{ADRContent: "adr", CodeContext: "+x", Filename: "a.go", Diff: true}
	if _, _, err := AnalyzeDrift(t.Context(), chat, in, "my custom system prompt"); err != nil {
		t.Fatalf("AnalyzeDrift: %v", err)
	}

	if gotSystem != "my custom system prompt" || !strings.Contains(gotUser, "unified diff") {
		t.Fatalf("system = %q; the user prompt must carry the diff instructions, got:\n%s", gotSystem, gotUser)
	}
}

func TestAnalyzeDrift_WholeFileInputKeepsTheWholeFileTemplate(t *testing.T) {
	var gotUser string

	chat := &MockProvider{ChatFunc: func(_ context.Context, _, user string) (string, error) {
		gotUser = user

		return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
	}}

	if _, _, err := AnalyzeDrift(t.Context(), chat, DriftInput{ADRContent: "adr", CodeContext: "x", Filename: "a.go"}, "system"); err != nil {
		t.Fatalf("AnalyzeDrift: %v", err)
	}

	if strings.Contains(gotUser, "unified diff") {
		t.Fatalf("a whole-file check must not mention a diff, got:\n%s", gotUser)
	}
}

func TestAnalyzeDrift_QuotedCodeWithBackslashes(t *testing.T) {
	tests := []struct {
		name       string
		quotedCode string
		want       string
	}{
		{name: "shell escaped space", quotedCode: `^git\ (diff|log)$args`, want: `^git\ (diff|log)$args`},
		{name: "shell escaped semicolon", quotedCode: `[^\;\&\|]*`, want: `[^\;\&\|]*`},
		{name: "u without four hex digits", quotedCode: `C:\users`, want: `C:\users`},
		{name: "valid escapes still decode", quotedCode: `a\nb \\ \u0041 \"`, want: "a\nb \\ A \""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := &MockProvider{ChatFunc: func(_ context.Context, _, _ string) (string, error) {
				return `{"violation": true, "reasoning": "r", "quoted_code": "` + tt.quotedCode + `"}`, nil
			}}

			got, _, err := AnalyzeDrift(t.Context(), chat, DriftInput{ADRContent: "adr", CodeContext: "x", Filename: "a.sh"}, "system")
			if err != nil {
				t.Fatalf("AnalyzeDrift: %v", err)
			}

			if !got.Violation || got.QuotedCode != tt.want {
				t.Fatalf("got violation %v, quoted code %q; want true, %q", got.Violation, got.QuotedCode, tt.want)
			}
		})
	}
}

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

	got, _, err := SuggestRemediation(
		t.Context(),
		provider,
		DriftInput{ADRContent: "adr content", CodeContext: "code content", Filename: "file.js"},
		AnalysisResult{Reasoning: "This file is JavaScript but the ADR mandates Go.", QuotedCode: "const x = 1;"},
	)
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

	_, _, err := SuggestRemediation(
		t.Context(),
		provider,
		DriftInput{ADRContent: "adr", CodeContext: "code", Filename: "file.go"},
		AnalysisResult{Reasoning: "reasoning", QuotedCode: "quoted"},
	)
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

	_, _, err := SuggestRemediation(
		t.Context(),
		provider,
		DriftInput{ADRContent: "All services must be Go.", CodeContext: "const x = 1;", Filename: "file.js"},
		AnalysisResult{Reasoning: "This file is JS but ADR mandates Go.", QuotedCode: "const x = 1;"},
	)
	if err != nil {
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

func TestAnalysisResult_HasNoSuggestionField(t *testing.T) {
	res := AnalysisResult{Violation: false, Reasoning: "no violation", QuotedCode: ""}

	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if strings.Contains(string(encoded), "suggestion") {
		t.Errorf("expected no suggestion field on AnalysisResult (suggestions are cached separately, see cache.ComputeSuggestionKey), got: %s", encoded)
	}
}
