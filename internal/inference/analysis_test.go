package inference

import (
	"context"
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
	if _, err := AnalyzeDrift(t.Context(), chat, in, "my custom system prompt"); err != nil {
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

	if _, err := AnalyzeDrift(t.Context(), chat, DriftInput{ADRContent: "adr", CodeContext: "x", Filename: "a.go"}, "system"); err != nil {
		t.Fatalf("AnalyzeDrift: %v", err)
	}

	if strings.Contains(gotUser, "unified diff") {
		t.Fatalf("a whole-file check must not mention a diff, got:\n%s", gotUser)
	}
}
