package main

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func TestCodeContextContainsTrigger(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{
			name:   "matches when trigger appears in code context",
			prompt: inference.GetAnalyzeDriftPrompt("ADR without trigger", "const s = \"password\";", "x.js"),
			want:   true,
		},
		{
			name:   "does not match when trigger appears only in ADR content",
			prompt: inference.GetAnalyzeDriftPrompt("Do not print passwords", "const s = \"token\";", "x.js"),
			want:   false,
		},
		{
			name:   "does not match when code context tags are missing",
			prompt: "random prompt content",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codeContextContainsTrigger(tt.prompt, testutil.MockViolationTrigger); got != tt.want {
				t.Fatalf("codeContextContainsTrigger() = %v, want %v", got, tt.want)
			}
		})
	}
}
