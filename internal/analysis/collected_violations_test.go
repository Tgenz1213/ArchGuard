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
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestRun_AnnotatesNewViolations(t *testing.T) {
	const quote = "import python_library"

	fileLevel := output.Annotation{File: "service.py", ADRID: "0001", ADRTitle: "Use Golang", Message: "Python is not allowed."}
	onLine2 := fileLevel
	onLine2.Line = 2

	tests := []struct {
		name       string
		content    analysis.ContentProvider
		maxTokens  int
		quotedCode string
		baselined  bool
		want       []output.Annotation
	}{
		{
			name:       "quote found in the file",
			content:    &diffCapableContentProvider{content: "package main\n" + quote + "\n"},
			quotedCode: quote,
			want:       []output.Annotation{onLine2},
		},
		{
			name:       "quote not found",
			content:    &diffCapableContentProvider{content: "package main\n" + quote + "\n"},
			quotedCode: "never in the file",
			want:       []output.Annotation{fileLevel},
		},
		{
			name: "LLM saw the diff",
			content: &diffCapableContentProvider{
				content: "a long preamble that pushes the file over the token limit\n" + quote + "\n",
				diff:    "@@ -1,1 +1,1 @@\n-old\n+" + quote + "\n",
			},
			maxTokens:  1,
			quotedCode: "+" + quote,
			want:       []output.Annotation{fileLevel},
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
			provider := &llm.MockProvider{
				ChatFunc: func(context.Context, string, string) (string, error) {
					return fmt.Sprintf(`{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": %q}`, tt.quotedCode), nil
				},
			}
			store := index.NewLocalStore(5)
			store.ADRs = []index.ADR{{
				ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.",
				Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
			}}
			cfg := &config.Config{LLM: config.LLMConfig{MaxTokens: tt.maxTokens}}

			engine := analysis.NewEngine(cfg, store, provider, provider, tt.content)
			engine.Out = output.Discard()

			if tt.baselined {
				engine.Baseline = baseline.New()
				engine.Baseline.Add(baseline.Entry{ADRID: "0001", File: "service.py", QuotedCode: tt.quotedCode})
			}

			runEngine(t, engine, len(tt.want) > 0)

			var got []output.Annotation
			for _, v := range engine.CollectedViolations {
				got = append(got, v.Annotation())
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("annotations = %+v, want %+v", got, tt.want)
			}
		})
	}
}
