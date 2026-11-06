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
	llmFile = "import python_library\n# a long tail that only the full file has, so it is too big for a small max_tokens\n"
	llmDiff = "@@ -0,0 +1 @@\n+import python_library\n"
)

// nodeLLMProvider flags every judged file and records what the judge and the suggestion were sent.
type nodeLLMProvider struct {
	inference.MockProvider
	judged, suggested []string
}

func newNodeLLMProvider() *nodeLLMProvider {
	p := &nodeLLMProvider{}
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

func nodeLLMEngine(t *testing.T, p *nodeLLMProvider, content analysis.ContentProvider) *analysis.Engine {
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

func TestRun_SizingFollowsTheJudgeLLM(t *testing.T) {
	p := newNodeLLMProvider()
	engine := nodeLLMEngine(t, p, &diffCapableContentProvider{content: llmFile, diff: llmDiff})
	engine.JudgeLLM = config.LLMConfig{Provider: "claude", Model: "big", MaxTokens: 10}

	_ = captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(p.judged) != 1 || strings.Contains(p.judged[0], "long tail") || !strings.Contains(p.judged[0], llmDiff) {
		t.Errorf("judged %q, want the diff: the file fits llm.max_tokens but not the judge's llm block's", p.judged)
	}
}

func TestRun_SystemPromptFollowsTheJudgeLLM(t *testing.T) {
	var systems []string

	p := newNodeLLMProvider()
	judge := p.ChatFunc
	p.ChatFunc = func(ctx context.Context, system, user string) (string, error) {
		systems = append(systems, system)
		return judge(ctx, system, user)
	}

	engine := nodeLLMEngine(t, p, &MockContentProvider{Files: map[string]string{"service.py": llmFile}})
	engine.Config.LLM.SystemPrompt = "the llm block's prompt"
	engine.JudgeLLM = config.LLMConfig{Provider: "claude", Model: "big", MaxTokens: 1000, SystemPrompt: "the judge's prompt"}

	_ = captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(systems) != 1 || systems[0] != "the judge's prompt" {
		t.Errorf("system prompts = %q, want the judge's llm block's", systems)
	}
}

func TestRun_SuggestLLMSizing(t *testing.T) {
	smallSuggest := config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 10}

	tests := []struct {
		name          string
		content       analysis.ContentProvider
		suggest       config.LLMConfig
		wantSuggested string
		wantWarning   string
	}{
		{"same llm as the judge sends the file", &diffCapableContentProvider{content: llmFile, diff: llmDiff}, config.LLMConfig{}, llmFile, ""},
		{"an llm that fits sends the file", &diffCapableContentProvider{content: llmFile, diff: llmDiff}, config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 1000}, llmFile, ""},
		{"too big sends the diff", &diffCapableContentProvider{content: llmFile, diff: llmDiff}, smallSuggest, llmDiff, ""},
		{"too big with no diff is skipped", &MockContentProvider{Files: map[string]string{"service.py": llmFile}}, smallSuggest, "", "no suggestion for service.py: it is over the suggest model's max_tokens (10) and has no diff that fits"},
		{"too big with a diff that is too big is skipped", &diffCapableContentProvider{content: llmFile, diff: llmFile + llmFile}, smallSuggest, "", "has no diff that fits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newNodeLLMProvider()
			engine := nodeLLMEngine(t, p, tt.content)
			engine.SuggestFixes = true
			engine.SuggestLLM = tt.suggest

			out := captureOutput(engine, func() { runEngine(t, engine, true) })

			if tt.wantSuggested == "" {
				if len(p.suggested) != 0 {
					t.Errorf("suggested %q, want no suggestion call", p.suggested)
				}
			} else if len(p.suggested) != 1 || !strings.Contains(p.suggested[0], tt.wantSuggested) || (tt.wantSuggested == llmDiff && strings.Contains(p.suggested[0], "long tail")) {
				t.Errorf("suggested %q, want it to carry %q", p.suggested, tt.wantSuggested)
			}

			if tt.wantWarning != "" && !strings.Contains(out, tt.wantWarning) {
				t.Errorf("output %q, want the warning %q", out, tt.wantWarning)
			}
		})
	}
}

func TestRun_CacheKeysFollowTheNodeLLMs(t *testing.T) {
	c, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New failed: %v", err)
	}

	strong := config.LLMConfig{Provider: "claude", Model: "big", MaxTokens: 1000}
	rerun := func(judge, suggest config.LLMConfig) *nodeLLMProvider {
		p := newNodeLLMProvider()
		engine := nodeLLMEngine(t, p, &MockContentProvider{Files: map[string]string{"service.py": llmFile}})
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
		{"same llms hit both caches", strong, strong, 0, 0},
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
	p := newNodeLLMProvider()
	engine := nodeLLMEngine(t, p, &MockContentProvider{Files: map[string]string{"service.py": llmFile}})
	engine.SuggestFixes = true
	engine.SuggestLLM = config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 10}

	suggest := newNodeLLMProvider()
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

func TestRun_SuggestionFromTheDiffIsCachedApart(t *testing.T) {
	c, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New failed: %v", err)
	}

	suggestWith := func(maxTokens int) *nodeLLMProvider {
		p := newNodeLLMProvider()
		engine := nodeLLMEngine(t, p, &diffCapableContentProvider{content: llmFile, diff: llmDiff})
		engine.Cache = c
		engine.SuggestFixes = true
		engine.SuggestLLM = config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: maxTokens}

		_ = captureOutput(engine, func() { runEngine(t, engine, true) })

		return p
	}

	if p := suggestWith(10); len(p.suggested) != 1 || strings.Contains(p.suggested[0], "long tail") {
		t.Fatalf("suggested %q, want one suggestion from the diff", p.suggested)
	}

	if p := suggestWith(1000); len(p.suggested) != 1 || !strings.Contains(p.suggested[0], "long tail") {
		t.Errorf("suggested %q, want a fresh suggestion from the whole file, not the one cached from the diff", p.suggested)
	}
}

func TestRun_SuggestionIsSizedOncePerFile(t *testing.T) {
	second := goLangADR()
	second.ID, second.Title = "0002", "Use Go everywhere"

	store := goLangStore()
	store.ADRs = append(store.ADRs, second)

	p := newNodeLLMProvider()
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "ollama", Model: "small", MaxTokens: 1000},
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}
	engine := newEngine(t, cfg, store, both(p), &fallbackOnlyContentProvider{files: map[string]string{"service.py": llmFile}})
	engine.Cache = nil
	engine.SuggestFixes = true
	engine.SuggestLLM = config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 10}

	counts := 0
	suggest := newNodeLLMProvider()
	suggest.CountTokensFunc = func(_ context.Context, text string) (int, error) {
		counts++
		return len(text) / 4, nil
	}
	engine.SuggestChat = suggest

	out := captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(engine.CollectedViolations) != 2 {
		t.Fatalf("violations = %d, want 2", len(engine.CollectedViolations))
	}

	if counts != 1 || strings.Count(out, "no suggestion for service.py") != 1 {
		t.Errorf("counted %d times and warned %d times, want once each per file", counts, strings.Count(out, "no suggestion for service.py"))
	}
}

func TestRun_SuggestSkipNamesWhatWasJudged(t *testing.T) {
	p := newNodeLLMProvider()
	engine := nodeLLMEngine(t, p, &fallbackOnlyContentProvider{files: map[string]string{"service.py": llmFile}})
	engine.Config.LLM.MaxTokens = 15
	engine.SuggestFixes = true
	engine.SuggestLLM = config.LLMConfig{Provider: "openai", Model: "tiny", MaxTokens: 2}

	out := captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(p.suggested) != 0 || !strings.Contains(out, "no suggestion for service.py: what was judged is over the suggest model's max_tokens (2)") {
		t.Errorf("suggested %q with output %q, want the truncated file skipped with its warning", p.suggested, out)
	}
}

func TestRun_AnOversizedDiffIsSizedForTheSuggestionToo(t *testing.T) {
	bigDiff := "@@ -0,0 +1,2 @@\n+import python_library\n+" + strings.Repeat("x", 200) + "\n"

	p := newNodeLLMProvider()
	engine := nodeLLMEngine(t, p, &diffCapableContentProvider{content: llmFile + strings.Repeat("y", 4000), diff: bigDiff})
	engine.Config.LLM.MaxTokens = 20
	engine.SuggestFixes = true

	out := captureOutput(engine, func() { runEngine(t, engine, true) })

	if len(p.suggested) != 0 || !strings.Contains(out, "no suggestion for service.py: what was judged is over the suggest model's max_tokens (20)") {
		t.Errorf("suggested %q with output %q, want the oversized diff skipped even on the judge's own llm", p.suggested, out)
	}
}
