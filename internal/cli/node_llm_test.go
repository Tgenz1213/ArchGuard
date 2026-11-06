package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

type namedChat struct{ name string }

func (c namedChat) Chat(context.Context, string, string) (inference.ChatReply, error) {
	return inference.ChatReply{}, nil
}

func (c namedChat) CountTokens(context.Context, string) (int, error) { return 0, nil }

func (c namedChat) CreateEmbedding(context.Context, string, inference.EmbeddingTaskType) (inference.Embedding, error) {
	return inference.Embedding{}, nil
}

func TestNodeKeyEnv(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "ollama"}}

	tests := []struct {
		provider string
		want     string
	}{
		{"ollama", "ARCHGUARD_API_KEY"},
		{"claude", "ARCHGUARD_CLAUDE_API_KEY"},
		{"openai", "ARCHGUARD_OPENAI_API_KEY"},
	}
	for _, tt := range tests {
		if got := nodeKeyEnv(cfg, config.LLMConfig{Provider: tt.provider}); got != tt.want {
			t.Errorf("nodeKeyEnv(%s) = %s, want %s", tt.provider, got, tt.want)
		}
	}
}

func TestNodeChatter_DifferentProviderNeverReadsTheMainKey(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "ollama"}}
	judge := config.LLMConfig{Provider: "claude", Model: "big"}

	t.Setenv("ARCHGUARD_API_KEY", "main-key")
	t.Setenv("ARCHGUARD_CLAUDE_API_KEY", "")

	var warnings bytes.Buffer
	if _, err := nodeChatter(output.New(&warnings, false), cfg, judge, ProviderFactories{}); err != nil {
		t.Fatalf("nodeChatter failed: %v", err)
	}

	if !strings.Contains(warnings.String(), "no API key set for claude") {
		t.Errorf("warnings = %q, want the missing-key warning, since ARCHGUARD_API_KEY belongs to llm.provider", warnings.String())
	}

	t.Setenv("ARCHGUARD_CLAUDE_API_KEY", "claude-key")
	warnings.Reset()

	if _, err := nodeChatter(output.New(&warnings, false), cfg, judge, ProviderFactories{}); err != nil {
		t.Fatalf("nodeChatter failed: %v", err)
	}

	if warnings.Len() != 0 {
		t.Errorf("warnings = %q, want none with ARCHGUARD_CLAUDE_API_KEY set", warnings.String())
	}
}

func TestNodeChatter_FactoryGetsTheNodeLLM(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "ollama", Model: "small"}}

	var got config.LLMConfig
	factories := ProviderFactories{Chat: func(c *config.Config) inference.Provider {
		got = c.LLM
		return namedChat{}
	}}

	if _, err := nodeChatter(output.New(&bytes.Buffer{}, false), cfg, config.LLMConfig{Provider: "claude", Model: "big"}, factories); err != nil {
		t.Fatalf("nodeChatter failed: %v", err)
	}

	if got.Provider != "claude" || got.Model != "big" {
		t.Errorf("factory got %+v, want the node's llm block", got)
	}

	if cfg.LLM.Model != "small" {
		t.Errorf("cfg.LLM.Model = %q, want it unchanged", cfg.LLM.Model)
	}
}

func pipelineNodes(judge config.LLMConfig, suggest *config.Node) []config.Node {
	nodes := []config.Node{
		{Name: "shortlist", Job: config.JobRank},
		{Name: "judge", Job: config.JobJudge, OwnLLM: judge.Model != "small", LLM: judge},
	}
	if suggest != nil {
		nodes = append(nodes, *suggest)
	}

	return nodes
}

func setupWithNodeLLMs(built *[]string) runSetup {
	main := config.LLMConfig{Provider: "ollama", Model: "small"}

	return runSetup{
		cfg:  &config.Config{LLM: main},
		chat: namedChat{name: "main"},
		chatFor: func(llm config.LLMConfig) (inference.Chatter, error) {
			*built = append(*built, llm.Model)
			return namedChat{name: llm.Model}, nil
		},
	}
}

func TestWithNodeLLMs(t *testing.T) {
	main := config.LLMConfig{Provider: "ollama", Model: "small"}
	big := config.LLMConfig{Provider: "claude", Model: "big"}
	tiny := config.LLMConfig{Provider: "openai", Model: "tiny"}

	tests := []struct {
		name        string
		nodes       []config.Node
		wantJudge   string
		wantSuggest string
		wantBuilt   []string
	}{
		{"no node llm blocks", pipelineNodes(main, nil), "main", "main", nil},
		{"judge llm block writes fixes too", pipelineNodes(big, nil), "big", "big", []string{"big"}},
		{"suggest on the llm block", pipelineNodes(big, &config.Node{Name: "suggest", Job: config.JobSuggest, LLM: main}), "big", "main", []string{"big"}},
		{"suggest with its own llm block", pipelineNodes(main, &config.Node{Name: "suggest", Job: config.JobSuggest, OwnLLM: true, LLM: tiny}), "main", "tiny", []string{"tiny"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var built []string

			setup, err := withNodeLLMs(setupWithNodeLLMs(&built), tt.nodes, true)
			if err != nil {
				t.Fatalf("withNodeLLMs failed: %v", err)
			}

			if got := setup.chat.(namedChat).name; got != tt.wantJudge {
				t.Errorf("judge chat = %s, want %s", got, tt.wantJudge)
			}

			if got := setup.suggestChat.(namedChat).name; got != tt.wantSuggest {
				t.Errorf("suggest chat = %s, want %s", got, tt.wantSuggest)
			}

			if strings.Join(built, ",") != strings.Join(tt.wantBuilt, ",") {
				t.Errorf("built %v, want %v", built, tt.wantBuilt)
			}

			if setup.judgeLLM != tt.nodes[1].LLM || setup.suggestLLM != config.SuggestNode(tt.nodes).LLM {
				t.Errorf("llms = %+v and %+v, want the judge's and the suggest node's", setup.judgeLLM, setup.suggestLLM)
			}
		})
	}
}

func TestWithNodeLLMs_BuildsNoSuggestChatWithoutSuggestFixes(t *testing.T) {
	var built []string

	nodes := pipelineNodes(config.LLMConfig{Provider: "ollama", Model: "small"}, &config.Node{Name: "suggest", Job: config.JobSuggest, OwnLLM: true, LLM: config.LLMConfig{Provider: "openai", Model: "tiny"}})

	setup, err := withNodeLLMs(setupWithNodeLLMs(&built), nodes, false)
	if err != nil {
		t.Fatalf("withNodeLLMs failed: %v", err)
	}

	if len(built) != 0 || setup.suggestChat.(namedChat).name != "main" {
		t.Errorf("built %v with suggest chat %v, want nothing built, so an unused llm block's missing key never warns", built, setup.suggestChat)
	}
}

func TestWithNodeLLMs_NamesTheNodeOnABuildError(t *testing.T) {
	setup := runSetup{
		chat:    namedChat{name: "main"},
		chatFor: func(config.LLMConfig) (inference.Chatter, error) { return nil, errors.New("unknown provider: voyage") },
	}

	_, err := withNodeLLMs(setup, pipelineNodes(config.LLMConfig{Provider: "voyage"}, nil), true)
	if err == nil || !strings.Contains(err.Error(), `analysis.pipeline node "judge".llm: unknown provider: voyage`) {
		t.Errorf("err = %v, want the build error naming the judge's llm block", err)
	}
}

func TestUsageReport_PricesEachRoleWithItsLLM(t *testing.T) {
	reported := inference.Usage{InputTokens: 2000, OutputTokens: 500, Reported: true}

	tally := &telemetry.Tally{}
	tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: reported})
	tally.Record(telemetry.RequestSent{Role: telemetry.Suggestion, Usage: reported})

	cfg := &config.Config{LLM: config.LLMConfig{CostPer1KInputTokens: new(1.0), CostPer1KOutputTokens: new(1.0)}}
	llms := roleLLMs{
		judge:   config.LLMConfig{CostPer1KInputTokens: new(0.003), CostPer1KOutputTokens: new(0.015)},
		suggest: config.LLMConfig{CostPer1KInputTokens: new(0.001), CostPer1KOutputTokens: new(0.002)},
	}

	usage := usageReport(cfg, llms, true, tally)

	assertCost(t, "judgment", usage.Judgment.Cost, 0.0135)
	assertCost(t, "suggestion", usage.Suggestion.Cost, 0.003)
}

func TestDryRunUsage_PricesWithTheJudgeLLM(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{CostPer1KInputTokens: new(1.0)}}
	judge := config.LLMConfig{CostPer1KInputTokens: new(0.003)}

	usage := dryRunUsage(cfg, judge, &telemetry.Tally{}, inference.Usage{InputTokens: 2000, Reported: true})

	assertCost(t, "judgment", usage.Judgment.Cost, 0.006)
}
