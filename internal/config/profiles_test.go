package config

import (
	"strings"
	"testing"
)

const strongProfile = "llms:\n  strong:\n    provider: claude\n    model: big\n    max_tokens: 150000\n    cost_per_1k_input_tokens: 0.003\n"

const suggestNode = `
    - name: suggest
      job: suggest
      with: llm
`

func TestParsePipeline_JudgeAndSuggestTakeProfiles(t *testing.T) {
	top := "llm:\n  provider: ollama\n  model: small\n" + strongProfile
	body := "  pipeline:" + shortlistNode + judgeNode + "      llm: strong\n" + suggestNode

	nodes, err := parseFromYAML(t, top, body)
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	judge := JudgeNode(nodes)
	if judge.Profile != "strong" || judge.LLM.Provider != "claude" || judge.LLM.Model != "big" || judge.LLM.MaxTokens != 150000 || judge.LLM.CostPer1KInputTokens == nil || *judge.LLM.CostPer1KInputTokens != 0.003 {
		t.Errorf("judge = %+v, want the strong profile", judge)
	}

	suggest := SuggestNode(nodes)
	if suggest.Name != "suggest" || suggest.Profile != "" || suggest.LLM.Provider != "ollama" || suggest.LLM.Model != "small" {
		t.Errorf("suggest = %+v, want the llm block", suggest)
	}
}

func TestSuggestNode_FallsBackToTheJudge(t *testing.T) {
	nodes, err := parseFromYAML(t, strongProfile, "  pipeline:"+shortlistNode+judgeNode+"      llm: strong\n")
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	if got := SuggestNode(nodes); got.Name != "judge" || got.Profile != "strong" {
		t.Errorf("SuggestNode = %+v, want the judge with its profile", got)
	}
}

func TestParsePipeline_SuggestTakesAProfile(t *testing.T) {
	nodes, err := parseFromYAML(t, strongProfile, "  pipeline:"+shortlistNode+judgeNode+suggestNode+"      llm: strong\n")
	if err != nil {
		t.Fatalf("ParsePipeline failed: %v", err)
	}

	if got := SuggestNode(nodes); got.Profile != "strong" || got.LLM.Provider != "claude" {
		t.Errorf("SuggestNode = %+v, want the strong profile", got)
	}
}

func TestParsePipeline_EmptyLLMsBlock(t *testing.T) {
	for _, top := range []string{"", "llms:\n", "llms: {}\n"} {
		if _, err := parseFromYAML(t, top, "  pipeline:"+shortlistNode+judgeNode); err != nil {
			t.Errorf("%q: ParsePipeline failed: %v", top, err)
		}
	}
}

func TestParsePipeline_InvalidProfiles(t *testing.T) {
	withJudgeProfile := "  pipeline:" + shortlistNode + judgeNode + "      llm: strong\n"
	tests := []struct {
		name string
		top  string
		body string
		want []string
	}{
		{"llms not a mapping", "llms: [strong]\n", withJudgeProfile, []string{"llms", "mapping"}},
		{"profile not a mapping", "llms:\n  strong: claude\n", withJudgeProfile, []string{"llms.strong", "mapping"}},
		{"bad profile name", "llms:\n  \"two words\":\n    provider: claude\n", withJudgeProfile, []string{`llms.two words`, "letters, digits"}},
		{"unknown key", "llms:\n  strong:\n    provider: claude\n    modle: big\n", withJudgeProfile, []string{"llms.strong", `"modle"`}},
		{"missing provider", "llms:\n  strong:\n    model: big\n", withJudgeProfile, []string{"llms.strong", "provider is required"}},
		{"wrong type", "llms:\n  strong:\n    provider: claude\n    max_tokens: lots\n", withJudgeProfile, []string{"llms.strong", "`lots`"}},
		{"unknown profile", strongProfile, "  pipeline:" + shortlistNode + judgeNode + "      llm: weak\n", []string{`node "judge".llm`, `"weak"`, "available: strong"}},
		{"no profiles at all", "", withJudgeProfile, []string{`node "judge".llm`, `"strong"`, "add it under llms:"}},
		{"unknown profile on suggest", strongProfile, "  pipeline:" + shortlistNode + judgeNode + suggestNode + "      llm: weak\n", []string{`node "suggest".llm`, `"weak"`}},
		{"non-string llm", strongProfile, "  pipeline:" + shortlistNode + judgeNode + "      llm: [strong]\n", []string{`node "judge".llm`}},
		{"llm on a rank node", strongProfile, "  pipeline:" + shortlistNode + "      llm: strong\n" + judgeNode, []string{`node "shortlist"`, `"llm"`, "not allowed"}},
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

func TestLoadConfig_AcceptsABadLLMsBlock(t *testing.T) {
	for _, body := range []string{"llms: [strong]\n", "llms:\n  strong:\n    modle: big\n"} {
		if _, err := loadFromYAML(t, body); err != nil {
			t.Errorf("%q: LoadConfig failed: %v", body, err)
		}
	}
}
