package analysis_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
)

const (
	profileFile = "import python_library\n# a long tail that only the full file has, so it is too big for a small max_tokens\n"
	profileDiff = "@@ -0,0 +1 @@\n+import python_library\n"
)

// profileProvider flags every judged file and records what the judge and the suggestion were sent.
type profileProvider struct {
	inference.MockProvider
	judged, suggested []string
}

func newProfileProvider() *profileProvider {
	p := &profileProvider{}
	p.ChatFunc = func(_ context.Context, system, user string) (string, error) {
		if strings.Contains(system, "Remediation Advisor") {
			p.suggested = append(p.suggested, user)
			return `{"suggestion": "Use Go."}`, nil
		}

		p.judged = append(p.judged, user)

		return `{"violation": true, "reasoning": "Python is not allowed.", "quoted_code": "import python_library"}`, nil
	}

	return p
}

func profileEngine(t *testing.T, p *profileProvider, content analysis.ContentProvider) *analysis.Engine {
	t.Helper()

	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "ollama", Model: "small", MaxTokens: 1000},
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	engine := newEngine(t, cfg, goLangStore(), both(p), content)
	engine.Cache = nil

	return engine
}

func TestRun_SizingFollowsTheJudgeProfile(t *testing.T) {
	p := newProfileProvider()
	engine := profileEngine(t, p, &diffCapableContentProvider{content: profileFile, diff: profileDiff})
	engine.JudgeLLM = config.LLMConfig{Provider: "claude", Model: "big", MaxTokens: 10}

	_ = captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(p.judged) != 1 || strings.Contains(p.judged[0], "long tail") || !strings.Contains(p.judged[0], profileDiff) {
		t.Errorf("judged %q, want the diff: the file fits llm.max_tokens but not the judge profile's", p.judged)
	}
}

func TestRun_SystemPromptFollowsTheJudgeProfile(t *testing.T) {
	var systems []string

	p := newProfileProvider()
	judge := p.ChatFunc
	p.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
		systems = append(systems, system)
		return judge(ctx, system, user)
	}

	engine := profileEngine(t, p, &MockContentProvider{Files: map[string]string{"service.py": profileFile}})
	engine.Config.LLM.SystemPrompt = "the llm block's prompt"
	engine.JudgeLLM = config.LLMConfig{Provider: "claude", Model: "big", MaxTokens: 1000, SystemPrompt: "the profile's prompt"}

	_ = captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(systems) != 1 || systems[0] != "the profile's prompt" {
		t.Errorf("system prompts = %q, want the judge profile's", systems)
	}
}

func TestRun_SuggestProfileSizing(t *testing.T) {
	smallSuggest := config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 10}

	tests := []struct {
		name          string
		content       analysis.ContentProvider
		suggest       config.LLMConfig
		wantSuggested string
		wantWarning   string
	}{
		{"same profile as the judge sends the file", &diffCapableContentProvider{content: profileFile, diff: profileDiff}, config.LLMConfig{}, profileFile, ""},
		{"a profile that fits sends the file", &diffCapableContentProvider{content: profileFile, diff: profileDiff}, config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 1000}, profileFile, ""},
		{"too big sends the diff", &diffCapableContentProvider{content: profileFile, diff: profileDiff}, smallSuggest, profileDiff, ""},
		{"too big with no diff is skipped", &MockContentProvider{Files: map[string]string{"service.py": profileFile}}, smallSuggest, "", "no suggestion for service.py: it is over the suggest model's max_tokens (10) and has no diff that fits"},
		{"too big with a diff that is too big is skipped", &diffCapableContentProvider{content: profileFile, diff: profileFile + profileFile}, smallSuggest, "", "has no diff that fits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProfileProvider()
			engine := profileEngine(t, p, tt.content)
			engine.SuggestFixes = true
			engine.SuggestLLM = tt.suggest

			out := captureOutput(engine, func() { runEngine(t, engine, true) })

			if tt.wantSuggested == "" {
				if len(p.suggested) != 0 {
					t.Errorf("suggested %q, want no suggestion call", p.suggested)
				}
			} else if len(p.suggested) != 1 || !strings.Contains(p.suggested[0], tt.wantSuggested) || (tt.wantSuggested == profileDiff && strings.Contains(p.suggested[0], "long tail")) {
				t.Errorf("suggested %q, want it to carry %q", p.suggested, tt.wantSuggested)
			}

			if tt.wantWarning != "" && !strings.Contains(out, tt.wantWarning) {
				t.Errorf("output %q, want the warning %q", out, tt.wantWarning)
			}
		})
	}
}

func TestRun_CacheKeysFollowTheProfiles(t *testing.T) {
	c, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New failed: %v", err)
	}

	strong := config.LLMConfig{Provider: "claude", Model: "big", MaxTokens: 1000}
	rerun := func(judge, suggest config.LLMConfig) *profileProvider {
		p := newProfileProvider()
		engine := profileEngine(t, p, &MockContentProvider{Files: map[string]string{"service.py": profileFile}})
		engine.Cache = c
		engine.SuggestFixes = true
		engine.JudgeLLM, engine.SuggestLLM = judge, suggest

		_ = captureOutput(engine, func() { runEngine(t, engine, true) })

		return p
	}

	tests := []struct {
		name          string
		judge         config.LLMConfig
		suggest       config.LLMConfig
		wantJudged    int
		wantSuggested int
	}{
		{"first run", strong, strong, 1, 1},
		{"same profiles hit both caches", strong, strong, 0, 0},
		{"another judge provider misses the analysis cache", config.LLMConfig{Provider: "openai", Model: "big", MaxTokens: 1000}, strong, 1, 0},
		{"another judge model misses the analysis cache", config.LLMConfig{Provider: "claude", Model: "bigger", MaxTokens: 1000}, strong, 1, 0},
		{"another suggest provider misses the suggestion cache", strong, config.LLMConfig{Provider: "openai", Model: "big", MaxTokens: 1000}, 0, 1},
		{"another suggest model misses the suggestion cache", strong, config.LLMConfig{Provider: "claude", Model: "bigger", MaxTokens: 1000}, 0, 1},
	}
	for _, tt := range tests {
		p := rerun(tt.judge, tt.suggest)

		if len(p.judged) != tt.wantJudged || len(p.suggested) != tt.wantSuggested {
			t.Errorf("%s: judged %d and suggested %d times, want %d and %d", tt.name, len(p.judged), len(p.suggested), tt.wantJudged, tt.wantSuggested)
		}
	}
}

func TestRun_SuggestCountErrorSkipsTheSuggestion(t *testing.T) {
	p := newProfileProvider()
	engine := profileEngine(t, p, &MockContentProvider{Files: map[string]string{"service.py": profileFile}})
	engine.SuggestFixes = true
	engine.SuggestLLM = config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 10}

	suggest := newProfileProvider()
	suggest.CountTokensFunc = func(context.Context, string) (int, error) { return 0, errors.New("tokenizer offline") }
	engine.SuggestChat = suggest

	out := captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(suggest.suggested) != 0 {
		t.Errorf("suggested %q, want no suggestion call", suggest.suggested)
	}

	if !strings.Contains(out, "no suggestion for service.py: counting tokens: tokenizer offline") {
		t.Errorf("output %q, want the count error named with the file", out)
	}
}
