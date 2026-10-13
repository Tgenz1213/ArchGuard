package analysis_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestRun_AnnotatesNewFindings(t *testing.T) {
	const quote = "import python_library"

	fileLevel := output.Annotation{File: "service.py", ADRID: "0001", ADRTitle: "Use Golang", Message: "Python is not allowed."}
	onLine2 := fileLevel
	onLine2.Line = 2
	warning := fileLevel
	warning.Warning = true

	tests := []struct {
		name       string
		content    analysis.ContentProvider
		maxTokens  int
		quotedCode string
		baselined  bool
		wantDrift  bool
		want       []output.Annotation
	}{
		{
			name:       "quote found in the file",
			content:    &diffCapableContentProvider{content: "package main\n" + quote + "\n"},
			quotedCode: quote,
			wantDrift:  true,
			want:       []output.Annotation{onLine2},
		},
		{
			name:       "quote not found",
			content:    &diffCapableContentProvider{content: "package main\n" + quote + "\n"},
			quotedCode: "never in the file",
			want:       []output.Annotation{warning},
		},
		{
			name: "LLM saw the diff",
			content: &diffCapableContentProvider{
				content: "a long preamble that pushes the file over the token limit\n" + quote + "\n",
				diff:    "@@ -1,2 +1,2 @@\n a long preamble that pushes the file over the token limit\n-old\n+" + quote + "\n",
			},
			maxTokens:  1,
			quotedCode: "+" + quote,
			wantDrift:  true,
			want:       []output.Annotation{onLine2},
		},
		{
			name:       "baselined",
			content:    &diffCapableContentProvider{content: "package main\n" + quote + "\n"},
			quotedCode: quote,
			baselined:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &inference.MockProvider{
				ChatFunc: func(context.Context, string, string) (string, error) {
					return fmt.Sprintf(`{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": %q}`, tt.quotedCode), nil
				},
			}
			store := index.NewLocalStore(5)
			store.ADRs = []index.ADR{{
				ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
				Embedding: unitVector(),
			}}
			cfg := &config.Config{LLM: config.LLMConfig{MaxTokens: tt.maxTokens}}

			engine := analysis.NewEngine(cfg, store, provider, provider, tt.content)
			engine.Out = output.Discard()

			if tt.baselined {
				engine.Baseline = baseline.New()
				engine.Baseline.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: tt.quotedCode})
			}

			runEngine(t, engine, tt.wantDrift)

			var got []output.Annotation
			for _, v := range slices.Concat(engine.CollectedViolations, engine.UnverifiedFindings) {
				got = append(got, v.Annotation())
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("annotations = %+v, want %+v", got, tt.want)
			}
		})
	}
}
