package output_test

import (
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func TestReportUsageLine(t *testing.T) {
	judged, generated, embedded := 1200, 80, 300

	tests := []struct {
		name  string
		usage *output.Usage
		want  string
	}{
		{
			name: "tokens and costs",
			usage: &output.Usage{
				Judgment:   output.ChatUsage{InputTokens: &judged, OutputTokens: &generated, Cost: new(0.0048)},
				Suggestion: &output.ChatUsage{InputTokens: &judged, OutputTokens: &generated, Cost: new(0.0048)},
				Embedding:  output.EmbeddingUsage{Tokens: &embedded, Cost: new(0.00003)},
			},
			want: "Tokens: judgment 1200 input, 80 output (estimated cost 0.004800); suggestions 1200 input, 80 output (estimated cost 0.004800); embeddings 300 (estimated cost 0.000030).\n",
		},
		{
			name: "unreported embedding",
			usage: &output.Usage{
				Judgment: output.ChatUsage{InputTokens: &judged, OutputTokens: &generated},
			},
			want: "Tokens: judgment 1200 input, 80 output; embeddings unavailable.\n",
		},
		{
			name:  "unreported chat",
			usage: &output.Usage{Embedding: output.EmbeddingUsage{Tokens: &embedded}},
			want:  "Tokens: judgment unavailable; embeddings 300.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderReport(output.Report{Coverage: output.Coverage{FilesJudged: 1, ADRChecks: 1}, Usage: tt.usage})

			if !strings.HasSuffix(got, tt.want) {
				t.Errorf("report = %q, want it to end with %q", got, tt.want)
			}
		})
	}
}

func TestReportWithoutUsageHasNoTokenLine(t *testing.T) {
	if got := renderReport(output.Report{Coverage: output.Coverage{FilesJudged: 1, ADRChecks: 1}}); strings.Contains(got, "Tokens:") {
		t.Errorf("report = %q, want no token line", got)
	}
}
