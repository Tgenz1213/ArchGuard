package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

const baseLLM = "llm:\n  provider: ollama\n  model: small\n  max_tokens: 8000\n  temperature: 0.0\n  cost_per_1k_input_tokens: 0.5\n  cost_per_1k_output_tokens: 2\n"

const suggestNode = `
    - name: suggest
      job: suggest
      with: llm
`

const strongLLM = "      llm:\n        provider: claude\n        model: big\n        max_tokens: 150000\n"

func TestParsePipeline_NodeLLMReplacesOnlyTheKeysItSets(t *testing.T) {
	nodes, err := parseFromYAML(t, baseLLM, "  pipeline:"+shortlistNode+judgeNode+"      llm:\n        model: big\n        max_tokens: 150000\n"+suggestNode)
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	judge := JudgeNode(nodes)
	if judge.LLM.Provider != "ollama" || judge.LLM.Model != "big" || judge.LLM.MaxTokens != 150000 {
		t.Errorf("judge = %+v, want its own model and max_tokens on the llm block's provider", judge.LLM)
	}

	if judge.LLM.Temperature == nil || *judge.LLM.Temperature != 0 || judge.LLM.CostPer1KInputTokens == nil || *judge.LLM.CostPer1KInputTokens != 0.5 {
		t.Errorf("judge = %+v, want temperature and prices from the llm block", judge.LLM)
	}

	suggest := SuggestNode(nodes)
	if suggest.Name != "suggest" || suggest.LLM.Provider != "ollama" || suggest.LLM.Model != "small" {
		t.Errorf("suggest = %+v, want the llm block", suggest)
	}
}

func TestParsePipeline_NodeOnAnotherProviderStartsFresh(t *testing.T) {
	top := baseLLM + "  base_url: http://localhost:11434\n  system_prompt: be literal\n"

	nodes, err := parseFromYAML(t, top, "  pipeline:"+shortlistNode+judgeNode+strongLLM+suggestNode+"      llm:\n        provider: openai\n        model: tiny\n")
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	want := LLMConfig{Provider: "claude", Model: "big", MaxTokens: 150000, SystemPrompt: "be literal"}
	if got := JudgeNode(nodes).LLM; !reflect.DeepEqual(got, want) {
		t.Errorf("judge = %+v, want %+v: only system_prompt inherited", got, want)
	}

	if got := SuggestNode(nodes).LLM; !reflect.DeepEqual(got, LLMConfig{Provider: "openai", Model: "tiny"}) {
		t.Errorf("suggest = %+v, want nothing inherited", got)
	}
}

func TestParsePipeline_NodeNamingTheLLMBlocksProviderInheritsEverything(t *testing.T) {
	nodes, err := parseFromYAML(t, baseLLM, "  pipeline:"+shortlistNode+judgeNode+"      llm:\n        provider: ollama\n")
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	if got := JudgeNode(nodes).LLM; got.Model != "small" || got.MaxTokens != 8000 || got.CostPer1KOutputTokens == nil {
		t.Errorf("judge = %+v, want every key of the llm block", got)
	}
}

func TestParsePipeline_NodeLLMLeavesTheLLMBlockAlone(t *testing.T) {
	cfg, err := loadFromYAML(t, baseLLM+"vector_store:\n  similarity_threshold: 0.4\nanalysis:\n  pipeline:"+shortlistNode+judgeNode+"      llm:\n        temperature: 0.7\n")
	if err != nil {
		t.Fatal(err)
	}

	nodes, err := ParsePipeline(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if got := *JudgeNode(nodes).LLM.Temperature; got != 0.7 {
		t.Errorf("judge temperature = %v, want 0.7", got)
	}

	if got := *cfg.LLM.Temperature; got != 0 {
		t.Errorf("llm.temperature = %v, want it unchanged at 0", got)
	}
}

func TestSuggestNode_FallsBackToTheJudge(t *testing.T) {
	nodes, err := parseFromYAML(t, baseLLM, "  pipeline:"+shortlistNode+judgeNode+strongLLM)
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	if got := SuggestNode(nodes); got.Name != "judge" || got.LLM.Model != "big" {
		t.Errorf("SuggestNode = %+v, want the judge with its own llm block", got)
	}
}

func TestParsePipeline_SuggestTakesItsOwnLLM(t *testing.T) {
	nodes, err := parseFromYAML(t, baseLLM, "  pipeline:"+shortlistNode+judgeNode+suggestNode+"      llm:\n        model: tiny\n")
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	if got := SuggestNode(nodes); got.LLM.Provider != "ollama" || got.LLM.Model != "tiny" {
		t.Errorf("SuggestNode = %+v, want the llm block with model tiny", got)
	}

	if got := JudgeNode(nodes).LLM.Model; got != "small" {
		t.Errorf("judge model = %q, want the llm block's", got)
	}
}

func TestParsePipeline_InvalidNodeLLM(t *testing.T) {
	judgeWith := func(llm string) string { return "  pipeline:" + shortlistNode + judgeNode + llm }
	tests := []struct {
		name string
		top  string
		body string
		want []string
	}{
		{"not a mapping", baseLLM, judgeWith("      llm: strong\n"), []string{`node "judge".llm`, "mapping"}},
		{"unknown key", baseLLM, judgeWith("      llm:\n        modle: big\n"), []string{`node "judge".llm`, `"modle"`}},
		{"wrong type", baseLLM, judgeWith("      llm:\n        max_tokens: lots\n"), []string{`node "judge".llm`, "`lots`"}},
		{"one price over a block with none", "llm:\n  provider: ollama\n", judgeWith("      llm:\n        cost_per_1k_input_tokens: 0.003\n"), []string{`node "judge".llm.cost_per_1k_input_tokens`, "or neither"}},
		{"empty provider", baseLLM, judgeWith("      llm:\n        provider: \"\"\n"), []string{`node "judge".llm.provider`, "empty"}},
		{"system_prompt on suggest", baseLLM, "  pipeline:" + shortlistNode + judgeNode + suggestNode + "      llm:\n        system_prompt: be kind\n", []string{`node "suggest".llm`, `"system_prompt"`}},
		{"one price over a block with both", baseLLM, judgeWith("      llm:\n        cost_per_1k_input_tokens: 0.003\n"), []string{`node "judge".llm.cost_per_1k_input_tokens`, "or neither"}},
		{"another provider without a model", baseLLM, judgeWith("      llm:\n        provider: claude\n"), []string{`node "judge".llm`, "set model", `"claude"`}},
		{"another provider with an empty model", baseLLM, judgeWith("      llm:\n        provider: claude\n        model: \"\"\n"), []string{`node "judge".llm`, "set model"}},
		{"negative price", baseLLM, judgeWith("      llm:\n        cost_per_1k_input_tokens: 1\n        cost_per_1k_output_tokens: -1\n"), []string{`node "judge".llm.cost_per_1k_output_tokens`, "negative"}},
		{"bad key on suggest", baseLLM, "  pipeline:" + shortlistNode + judgeNode + suggestNode + "      llm:\n        modle: tiny\n", []string{`node "suggest".llm`, `"modle"`}},
		{"llm on a rank node", baseLLM, "  pipeline:" + shortlistNode + strongLLM + judgeNode, []string{`node "shortlist"`, `"llm"`, "not allowed"}},
		{"two suggest nodes", "", "  pipeline:" + shortlistNode + judgeNode + suggestNode + strings.ReplaceAll(suggestNode, "name: suggest", "name: suggest2"), []string{"at most one suggest", "found 2"}},
		{"suggest before the judge", "", "  pipeline:" + shortlistNode + suggestNode + judgeNode, []string{`"suggest"`, "before the judge"}},
		{"rank after the suggest", "", "  pipeline:" + shortlistNode + judgeNode + suggestNode + strings.ReplaceAll(shortlistNode, "shortlist", "late"), []string{`"late"`, "only a suggest node may follow the judge"}},
		{"wrong runner on suggest", "", "  pipeline:" + shortlistNode + judgeNode + strings.ReplaceAll(suggestNode, "with: llm", "with: jev"), []string{`node "suggest".with`, `"jev"`, "llm"}},
		{"key not allowed on suggest", "", "  pipeline:" + shortlistNode + judgeNode + suggestNode + "      on_error: skip\n", []string{`node "suggest"`, `"on_error"`, "not allowed"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFromYAML(t, tt.top, tt.body)
			if err == nil {
				t.Fatal("ParsePipeline succeeded, want an error")
			}

			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestParsePipeline_LeavesTheLLMBlocksPricesToItsOwnCheck(t *testing.T) {
	if _, err := parseFromYAML(t, "llm:\n  provider: ollama\n  cost_per_1k_input_tokens: 0.5\n", "  pipeline:"+shortlistNode+judgeNode+"      llm:\n        temperature: 0.7\n"); err != nil {
		t.Errorf("ParsePipeline failed: %v, want llm's one price left for llm's own check to name", err)
	}
}

func TestLoadConfig_AcceptsABadNodeLLM(t *testing.T) {
	if _, err := loadFromYAML(t, "analysis:\n  pipeline:"+shortlistNode+judgeNode+"      llm:\n        modle: big\n"); err != nil {
		t.Errorf("LoadConfig failed: %v", err)
	}
}

func TestLLMConfigClone_CopiesEveryPointer(t *testing.T) {
	top := LLMConfig{Temperature: new(0.1), CostPer1KInputTokens: new(0.2), CostPer1KOutputTokens: new(0.3)}
	clone := reflect.ValueOf(top.clone())

	for i, field := range slices.Collect(reflect.TypeFor[LLMConfig]().Fields()) {
		if field.Type.Kind() != reflect.Pointer {
			continue
		}

		if original := reflect.ValueOf(top).Field(i); original.IsNil() || original.Pointer() == clone.Field(i).Pointer() {
			t.Errorf("clone shares %s with the llm block, so decoding a node's block would change it", field.Name)
		}
	}
}
