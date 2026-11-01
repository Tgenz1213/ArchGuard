package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
	"gopkg.in/yaml.v3"
)

const splicedPipeline = `  pipeline:
    - name: shortlist
      job: rank
      with: cosine

    - name: judge
      job: judge
      with: llm
`

func TestSpliceDefaultPipeline(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		want         string
		wantPreJudge bool
	}{
		{
			name: "replaces a pre_judge mapping and keeps every other line",
			in: `version: "1" # keep me
analysis:
  adr_path: "./docs/arch"
  pipeline:
    pre_judge: # screens with Jev
      scorer: jev
      threshold: 0.3

  # Head comment of the next key.
  exclude_patterns:
    - "bin/**"
llm:
  provider: "ollama"
`,
			want: `version: "1" # keep me
analysis:
  adr_path: "./docs/arch"
` + splicedPipeline + `
  # Head comment of the next key.
  exclude_patterns:
    - "bin/**"
llm:
  provider: "ollama"
`,
			wantPreJudge: true,
		},
		{
			name:         "a commented-out line inside the old block goes with it",
			in:           "analysis:\n  pipeline:\n    rank:\n      top_k: 2\n#    rerank:\n#      top_k: 1\n    pre_judge:\n      scorer: jev\n  # Head comment.\n  adr_path: docs\n",
			want:         "analysis:\n" + splicedPipeline + "  # Head comment.\n  adr_path: docs\n",
			wantPreJudge: true,
		},
		{
			name: "the pipeline as the last block",
			in:   "analysis:\n  adr_path: docs\n  pipeline:\n    rank:\n      top_k: 2\n",
			want: "analysis:\n  adr_path: docs\n" + splicedPipeline,
		},
		{
			name: "a null pipeline",
			in:   "analysis:\n  pipeline:\n  adr_path: docs\n",
			want: "analysis:\n" + splicedPipeline + "  adr_path: docs\n",
		},
		{
			name: "no pipeline key appends it to the analysis block",
			in:   "analysis:\n  adr_path: docs\n  exclude_patterns:\n    - bin/**\n\n# trailing comment\nllm:\n  provider: ollama\n",
			want: "analysis:\n  adr_path: docs\n  exclude_patterns:\n    - bin/**\n" + splicedPipeline + "\n# trailing comment\nllm:\n  provider: ollama\n",
		},
		{
			name: "no analysis key appends one at the end",
			in:   "llm:\n  provider: ollama\n",
			want: "llm:\n  provider: ollama\nanalysis:\n" + splicedPipeline,
		},
		{
			name: "an empty config",
			in:   "",
			want: "analysis:\n" + splicedPipeline,
		},
		{
			name: "a config holding only a comment",
			in:   "# just a comment\n",
			want: "# just a comment\nanalysis:\n" + splicedPipeline,
		},
		{
			name: "matches a four-space indent",
			in:   "analysis:\n    adr_path: docs\n    pipeline:\n        rank:\n            top_k: 2\n",
			want: "analysis:\n    adr_path: docs\n    pipeline:\n        - name: shortlist\n          job: rank\n          with: cosine\n\n        - name: judge\n          job: judge\n          with: llm\n",
		},
		{
			name: "keeps CRLF line endings",
			in:   "analysis:\r\n  adr_path: docs\r\n  pipeline:\r\n    rank:\r\n      top_k: 2\r\nllm:\r\n  provider: ollama\r\n",
			want: strings.ReplaceAll("analysis:\n  adr_path: docs\n"+splicedPipeline+"llm:\n  provider: ollama\n", "\n", "\r\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splice, err := spliceDefaultPipeline([]byte(tt.in))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(splice.result) != tt.want {
				t.Errorf("result =\n%s\nwant\n%s", splice.result, tt.want)
			}

			if splice.hadPreJudge != tt.wantPreJudge {
				t.Errorf("hadPreJudge = %v, want %v", splice.hadPreJudge, tt.wantPreJudge)
			}

			assertPipelineParses(t, splice.result)
		})
	}
}

func assertPipelineParses(t *testing.T, content []byte) {
	t.Helper()

	var cfg config.Config
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		t.Fatalf("spliced config doesn't parse: %v", err)
	}

	if _, err := config.ParsePipeline(&cfg); err != nil {
		t.Errorf("spliced pipeline is invalid: %v", err)
	}
}

func TestSpliceDefaultPipeline_LeavesAListPipelineAlone(t *testing.T) {
	splice, err := spliceDefaultPipeline([]byte("analysis:\n" + splicedPipeline))
	if err != nil || !splice.alreadyList || splice.result != nil {
		t.Errorf("splice = %+v, err %v; want alreadyList and no result", splice, err)
	}
}

func TestSpliceDefaultPipeline_RefusesWhatItCantEditSafely(t *testing.T) {
	tests := []struct{ name, in string }{
		{"flow analysis", "analysis: {adr_path: docs, pipeline: {rank: {}}}\n"},
		{"flow pipeline", "analysis:\n  pipeline: {rank: {top_k: 2}}\n"},
		{"aliased analysis", "base: &base\n  adr_path: docs\nanalysis: *base\n"},
		{"merge key in analysis", "base: &base\n  adr_path: docs\nanalysis:\n  <<: *base\n  pipeline:\n    rank: {}\n"},
		{"anchored pipeline", "analysis:\n  pipeline: &p\n    rank:\n      top_k: 2\nother: *p\n"},
		{"anchor nested in the pipeline", "analysis:\n  pipeline:\n    rank: &r\n      top_k: 2\n  extra: *r\n"},
		{"analysis as an inline null", "version: 1\nanalysis: ~\nllm:\n  model: x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := spliceDefaultPipeline([]byte(tt.in)); !errors.Is(err, errNotSpliceable) {
				t.Errorf("err = %v, want errNotSpliceable", err)
			}
		})
	}
}

func TestGenerateConfig_HasAValidPipeline(t *testing.T) {
	assertPipelineParses(t, []byte(generateConfig("./docs/arch")))
}

func TestSpliceDefaultPipeline_ReturnsTheRemovedLines(t *testing.T) {
	splice, err := spliceDefaultPipeline([]byte("analysis:\n  pipeline:\n    rank:\n      threshold: 0.7\n  adr_path: docs\n"))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"  pipeline:", "    rank:", "      threshold: 0.7"}
	if !slices.Equal(splice.removed, want) {
		t.Errorf("removed = %q, want %q", splice.removed, want)
	}
}

func TestSpliceDefaultPipeline_ReportsAListCheckRejects(t *testing.T) {
	splice, err := spliceDefaultPipeline([]byte("analysis:\n  pipeline: []\n"))
	if err != nil || !splice.alreadyList || splice.listErr == nil {
		t.Errorf("splice = %+v, err %v; want alreadyList with the pipeline's error", splice, err)
	}
}
