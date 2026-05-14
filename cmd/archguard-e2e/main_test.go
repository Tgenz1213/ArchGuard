package main

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func TestCodeContextContainsTrigger(t *testing.T) {
	t.Run("matches when trigger appears in code context", func(t *testing.T) {
		prompt := llm.GetAnalyzeDriftPrompt("ADR without trigger", "const s = \"password\";", "x.js")
		if !codeContextContainsTrigger(prompt, testutil.MockViolationTrigger) {
			t.Fatalf("expected trigger match inside code_context")
		}
	})

	t.Run("does not match when trigger appears only in ADR content", func(t *testing.T) {
		prompt := llm.GetAnalyzeDriftPrompt("Do not print passwords", "const s = \"token\";", "x.js")
		if codeContextContainsTrigger(prompt, testutil.MockViolationTrigger) {
			t.Fatalf("expected no trigger match when only ADR contains trigger")
		}
	})

	t.Run("does not match when code context tags are missing", func(t *testing.T) {
		if codeContextContainsTrigger("random prompt content", testutil.MockViolationTrigger) {
			t.Fatalf("expected no trigger match without code_context tags")
		}
	})
}
