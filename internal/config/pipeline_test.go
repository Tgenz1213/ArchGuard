package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFromYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archguard.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return LoadConfig(path)
}

// parseFromYAML puts top before analysis and body inside it.
func parseFromYAML(t *testing.T, top, body string) ([]Node, error) {
	t.Helper()

	cfg, err := loadFromYAML(t, top+"vector_store:\n  similarity_threshold: 0.42\nanalysis:\n  max_relevant_adrs: 5\n"+body)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	return ParsePipeline(cfg)
}

const shortlistNode = `
    - name: shortlist
      job: rank
      with: cosine
`

const judgeNode = `
    - name: judge
      job: judge
      with: llm
`

func pipelineWith(middle string) string {
	return "  pipeline:" + shortlistNode + middle + judgeNode
}

func TestLoadConfig_AcceptsAnyPipelineAndJevBlock(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"absent", "analysis:\n  adr_path: docs\n"},
		{"retired mapping", "analysis:\n  pipeline:\n    pre_judge:\n      scorer: jev\n"},
		{"scalar", "analysis:\n  pipeline: cosine\n"},
		{"invalid node", "analysis:\n  pipeline:\n    - job: bake\n"},
		{"invalid jev block", "jev: [1]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadFromYAML(t, tt.yaml); err != nil {
				t.Errorf("LoadConfig failed: %v", err)
			}
		})
	}
}

func TestParsePipeline_DefaultPipeline(t *testing.T) {
	nodes, err := parseFromYAML(t, "", "  "+strings.ReplaceAll(DefaultPipeline, "\n", "\n  "))
	if err != nil {
		t.Fatal(err)
	}

	want := []Node{
		{Name: "shortlist", Job: JobRank, With: RunnerCosine, Threshold: 0.42, TopK: 5, OnError: OnErrorSkip},
		{Name: "judge", Job: JobJudge, With: RunnerLLM},
	}
	if len(nodes) != len(want) || nodes[0] != want[0] || nodes[1] != want[1] {
		t.Errorf("nodes = %+v, want %+v", nodes, want)
	}
}

func TestParsePipeline_RankTopKFallsBackToDefaultOfThree(t *testing.T) {
	cfg, err := loadFromYAML(t, "analysis:\n"+pipelineWith(""))
	if err != nil {
		t.Fatal(err)
	}

	nodes, err := ParsePipeline(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if nodes[0].TopK != 3 {
		t.Errorf("TopK = %d, want 3", nodes[0].TopK)
	}
}

func TestParsePipeline_RankSettings(t *testing.T) {
	nodes, err := parseFromYAML(t, "", pipelineWith(`
    - name: rerank
      job: rank
      with: cosine
      threshold: 0
      top_k: 2
      on_error: fail
`))
	if err != nil {
		t.Fatal(err)
	}

	want := Node{Name: "rerank", Job: JobRank, With: RunnerCosine, Threshold: 0, TopK: 2, OnError: OnErrorFail}
	if nodes[1] != want {
		t.Errorf("rerank = %+v, want %+v", nodes[1], want)
	}
}

func TestParsePipeline_AskWithPreset(t *testing.T) {
	nodes, err := parseFromYAML(t, "jev:\n  model: jev-1\n", pipelineWith(`
    - name: breaks-a-rule
      job: ask
      with: jev
      preset: breaks-a-rule
`))
	if err != nil {
		t.Fatal(err)
	}

	want := Node{
		Name: "breaks-a-rule", Job: JobAsk, With: RunnerJev, Threshold: 0.3, OnError: OnErrorSkip, Model: "jev-1",
		Question: Question{Type: QuestionNoul, ForEach: ForEachRule, Instructions: BreaksARuleInstructions},
	}
	if nodes[1] != want {
		t.Errorf("ask = %+v, want %+v", nodes[1], want)
	}
}

func TestParsePipeline_AskWithItsOwnQuestion(t *testing.T) {
	nodes, err := parseFromYAML(t, "jev:\n  model: jev-1\n", pipelineWith(`
    - name: own
      job: ask
      with: jev
      model: jev-2
      type: noul
      for_each: rule
      instructions: "Is {rule} broken? {rule} {not a placeholder}"
      keep:
        threshold: 0.6
        top_k: 2
      on_error: fail
`))
	if err != nil {
		t.Fatal(err)
	}

	want := Node{
		Name: "own", Job: JobAsk, With: RunnerJev, Threshold: 0.6, TopK: 2, OnError: OnErrorFail, Model: "jev-2",
		Question: Question{Type: QuestionNoul, ForEach: ForEachRule, Instructions: "Is {rule} broken? {rule} {not a placeholder}"},
	}
	if nodes[1] != want {
		t.Errorf("ask = %+v, want %+v", nodes[1], want)
	}
}

func TestParsePipeline_PresetKeepOverridesItsDefaults(t *testing.T) {
	nodes, err := parseFromYAML(t, "", pipelineWith(`
    - name: screen
      job: ask
      with: jev
      preset: breaks-a-rule
      keep:
        threshold: 0.5
        top_k: 1
`))
	if err != nil {
		t.Fatal(err)
	}

	if nodes[1].Threshold != 0.5 || nodes[1].TopK != 1 || nodes[1].Model != "" {
		t.Errorf("ask = %+v, want threshold 0.5, top_k 1 and no model", nodes[1])
	}
}

func TestParsePipeline_AliasesAndMergeKeys(t *testing.T) {
	nodes, err := parseFromYAML(t, "", `  pipeline:
    - &shortlist
      name: shortlist
      job: rank
      with: cosine
      top_k: 4
    - <<: *shortlist
      name: rerank
      top_k: 2
`+judgeNode)
	if err != nil {
		t.Fatal(err)
	}

	if nodes[1].Name != "rerank" || nodes[1].Job != JobRank || nodes[1].TopK != 2 {
		t.Errorf("rerank = %+v, want a rank node with top_k 2", nodes[1])
	}
}

func TestParsePipeline_NeedsInit(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"missing", "  adr_path: docs\n"},
		{"null", "  pipeline:\n"},
		{"retired mapping", "  pipeline:\n    rank:\n      top_k: 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFromYAML(t, "", tt.yaml)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			if !errors.Is(err, ErrNeedsInit) {
				t.Errorf("error %q is not ErrNeedsInit", err)
			}

			for _, want := range []string{"archguard init", "- name: shortlist"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestParsePipeline_Invalid(t *testing.T) {
	ask := "\n    - name: screen\n      job: ask\n      with: jev\n"
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{"not a list", "  pipeline: cosine\n", []string{"analysis.pipeline", "list"}},
		{"empty list", "  pipeline: []\n", []string{"exactly one judge", "found 0"}},
		{"no judge", "  pipeline:" + shortlistNode, []string{"exactly one judge", "found 0"}},
		{"two judges", pipelineWith(strings.ReplaceAll(judgeNode, "name: judge", "name: judge2")), []string{"exactly one judge", "found 2"}},
		{"node after the judge", "  pipeline:" + shortlistNode + judgeNode + strings.ReplaceAll(shortlistNode, "shortlist", "late"), []string{`"late"`, "after the judge"}},
		{"ask first", "  pipeline:" + ask + "      preset: breaks-a-rule\n" + judgeNode, []string{`"screen"`, "first node", "rank"}},
		{"duplicate names", pipelineWith(shortlistNode), []string{`"shortlist"`, "unique"}},
		{"node not a mapping", "  pipeline:\n    - rank\n" + judgeNode, []string{"node 1", "mapping"}},
		{"missing name", "  pipeline:\n    - job: rank\n      with: cosine\n" + judgeNode, []string{"node 1", "name is required"}},
		{"bad name characters", "  pipeline:\n    - name: \"short list\"\n      job: rank\n      with: cosine\n" + judgeNode, []string{"node 1.name", "letters, digits"}},
		{"name too long", "  pipeline:\n    - name: " + strings.Repeat("a", 65) + "\n      job: rank\n      with: cosine\n" + judgeNode, []string{"node 1.name", "at most 64"}},
		{"non-string name", "  pipeline:\n    - name: [a]\n      job: rank\n      with: cosine\n" + judgeNode, []string{"node 1.name", "non-empty string"}},
		{"missing job", pipelineWith("\n    - name: x\n      with: cosine\n"), []string{`node "x"`, "job is required"}},
		{"unknown job", pipelineWith("\n    - name: x\n      job: bake\n      with: cosine\n"), []string{`node "x".job`, `"bake"`, "rank, ask, judge"}},
		{"missing runner", pipelineWith("\n    - name: x\n      job: rank\n"), []string{`node "x"`, "with is required"}},
		{"wrong runner for the job", pipelineWith("\n    - name: x\n      job: rank\n      with: jev\n"), []string{`node "x".with`, `"jev"`, "cosine"}},
		{"key not allowed on rank", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      preset: breaks-a-rule\n"), []string{`node "x"`, `"preset"`, "not allowed"}},
		{"key not allowed on judge", "  pipeline:" + shortlistNode + judgeNode + "      on_error: skip\n", []string{`node "judge"`, `"on_error"`, "not allowed"}},
		{"criteria on ask", pipelineWith(ask + "      preset: breaks-a-rule\n      criteria: [a, b]\n"), []string{`node "screen"`, `"criteria"`, "not allowed"}},
		{"threshold above range", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      threshold: 1.5\n"), []string{`node "x".threshold`, "1.5", "between 0 and 1"}},
		{"null threshold", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      threshold:\n"), []string{`node "x".threshold`, "number"}},
		{"zero top_k", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      top_k: 0\n"), []string{`node "x".top_k`, "positive"}},
		{"non-integer top_k", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      top_k: 2.5\n"), []string{`node "x".top_k`, "integer"}},
		{"unknown on_error", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      on_error: warn\n"), []string{`node "x".on_error`, `"warn"`, "skip, fail"}},
		{"duplicate key", pipelineWith("\n    - name: x\n      job: rank\n      with: cosine\n      top_k: 1\n      top_k: 2\n"), []string{`node 2`, `"top_k" already defined`}},
		{"unknown preset", pipelineWith(ask + "      preset: governs\n"), []string{`node "screen".preset`, `"governs"`, "breaks-a-rule"}},
		{"type beside a preset", pipelineWith(ask + "      preset: breaks-a-rule\n      type: noul\n"), []string{`node "screen"`, "type", "beside a preset"}},
		{"instructions beside a preset", pipelineWith(ask + "      preset: breaks-a-rule\n      instructions: \"{rule}\"\n"), []string{`node "screen"`, "instructions", "beside a preset"}},
		{"no preset or question", pipelineWith(ask + "      keep:\n        threshold: 0.3\n"), []string{`node "screen"`, "type, for_each and instructions", "type is missing"}},
		{"missing instructions", pipelineWith(ask + "      type: noul\n      for_each: rule\n      keep:\n        threshold: 0.3\n"), []string{`node "screen"`, "instructions is missing"}},
		{"choice not supported yet", pipelineWith(ask + "      type: choice\n      for_each: rule\n      instructions: \"{rule}\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".type`, `"choice"`, "not supported yet"}},
		{"score not supported yet", pipelineWith(ask + "      type: score\n      for_each: rule\n      instructions: \"{rule}\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".type`, `"score"`, "not supported yet"}},
		{"unknown type", pipelineWith(ask + "      type: maybe\n      for_each: rule\n      instructions: \"{rule}\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".type`, `"maybe"`, "unknown"}},
		{"for_each adr not supported yet", pipelineWith(ask + "      type: noul\n      for_each: adr\n      instructions: \"{rule}\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".for_each`, `"adr"`, "not supported yet"}},
		{"instructions without rule", pipelineWith(ask + "      type: noul\n      for_each: rule\n      instructions: Is this bad?\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".instructions`, "{rule}"}},
		{"code placeholder", pipelineWith(ask + "      type: noul\n      for_each: rule\n      instructions: \"Does {code} break {rule}?\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".instructions`, "{code}", "this code"}},
		{"adr placeholder", pipelineWith(ask + "      type: noul\n      for_each: rule\n      instructions: \"Is {adr} broken by {rule}?\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".instructions`, "{adr}"}},
		{"unknown placeholder", pipelineWith(ask + "      type: noul\n      for_each: rule\n      instructions: \"{rule} in {file}\"\n      keep:\n        threshold: 0.3\n"), []string{`node "screen".instructions`, "{file}", "unknown"}},
		{"no keep without a preset", pipelineWith(ask + "      type: noul\n      for_each: rule\n      instructions: \"{rule}\"\n"), []string{`node "screen"`, "keep.threshold is required"}},
		{"keep without threshold", pipelineWith(ask + "      type: noul\n      for_each: rule\n      instructions: \"{rule}\"\n      keep:\n        top_k: 2\n"), []string{`node "screen".keep`, "threshold is required"}},
		{"keep not a mapping", pipelineWith(ask + "      preset: breaks-a-rule\n      keep: 0.3\n"), []string{`node "screen".keep`, "mapping"}},
		{"unknown keep key", pipelineWith(ask + "      preset: breaks-a-rule\n      keep:\n        options: [a]\n"), []string{`node "screen".keep`, `"options"`, "not allowed"}},
		{"empty model", pipelineWith(ask + "      preset: breaks-a-rule\n      model: \"\"\n"), []string{`node "screen".model`, "non-empty string"}},
		{"jev not a mapping", pipelineWith("") + "jev: jev-1\n", []string{"jev", "mapping"}},
		{"api key in the jev block", pipelineWith("") + "jev:\n  api_key: secret\n", []string{"jev", `"api_key"`}},
		{"non-string jev model", pipelineWith("") + "jev:\n  model: [a]\n", []string{"jev.model", "non-empty string"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFromYAML(t, "", tt.yaml)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
