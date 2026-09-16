package cache

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/llm"
)

func TestComputeSuggestionKey_StableForSameInputs(t *testing.T) {
	a := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "sys", SuggestionPromptTemplate: "tmpl"})
	b := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "sys", SuggestionPromptTemplate: "tmpl"})
	if a != b {
		t.Errorf("expected identical inputs to produce the same key, got %q and %q", a, b)
	}
}

func TestComputeSuggestionKey_ChangesWithSuggestionPrompt(t *testing.T) {
	a := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "old system prompt", SuggestionPromptTemplate: "old template"})
	b := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "new system prompt", SuggestionPromptTemplate: "old template"})
	if a == b {
		t.Error("expected a changed suggestion system prompt to change the key")
	}

	c := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "old system prompt", SuggestionPromptTemplate: "new template"})
	if a == c {
		t.Error("expected a changed suggestion prompt template to change the key")
	}
}

func TestComputeSuggestionKey_ChangesWithFilename(t *testing.T) {
	a := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "old/path.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "sys", SuggestionPromptTemplate: "tmpl"})
	b := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "new/path.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "sys", SuggestionPromptTemplate: "tmpl"})
	if a == b {
		t.Error("expected identical content under a different file path to change the key, since the rendered suggestion prompt includes the file path")
	}
}

func TestComputeSuggestionKey_NoAmbiguousFieldBoundaries(t *testing.T) {
	a := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "m", ADRContent: "a||b", FileContent: "c", Filename: "f", Reasoning: "r", QuotedCode: "q", SuggestionSystemPrompt: "s", SuggestionPromptTemplate: "t"})
	b := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "m", ADRContent: "a", FileContent: "b||c", Filename: "f", Reasoning: "r", QuotedCode: "q", SuggestionSystemPrompt: "s", SuggestionPromptTemplate: "t"})
	if a == b {
		t.Error("expected differently-split fields around a literal delimiter-like substring to produce different keys")
	}
}

func TestComputeSuggestionKey_IndependentOfAnalysisKey(t *testing.T) {
	analysisKey := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", SystemPrompt: "judgment system prompt", UserPromptTemplate: "judgment template"})
	suggestionKey := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "suggestion system prompt", SuggestionPromptTemplate: "suggestion template"})
	if analysisKey == suggestionKey {
		t.Error("expected analysis and suggestion keys to live in independent namespaces")
	}
}

// Fixed digests computed from the pre-#183 positional-argument implementation,
// pinning field order so a future field reorder can't slip past self-consistency checks alone.
func TestComputeSuggestionKey_MatchesPreRefactorDigest(t *testing.T) {
	got := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "sys", SuggestionPromptTemplate: "tmpl"})
	want := "3bbf79ea1021fe4efcbefa734d4fc80131eab86457b8b19af4560f798408b413"
	if got != want {
		t.Errorf("expected digest to match the pre-refactor field order, got %q want %q", got, want)
	}
}

func TestComputeAnalysisKey_StableForSameInputs(t *testing.T) {
	a := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", SystemPrompt: "sys", UserPromptTemplate: "tmpl"})
	b := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", SystemPrompt: "sys", UserPromptTemplate: "tmpl"})
	if a != b {
		t.Errorf("expected identical inputs to produce the same key, got %q and %q", a, b)
	}
}

func TestComputeAnalysisKey_NoAmbiguousFieldBoundaries(t *testing.T) {
	a := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "m", ADRContent: "rule-A", FileContent: "||package main", SystemPrompt: "s", UserPromptTemplate: "t"})
	b := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "m", ADRContent: "rule-A||", FileContent: "package main", SystemPrompt: "s", UserPromptTemplate: "t"})
	if a == b {
		t.Error("expected differently-split adrContent/fileContent around a literal delimiter-like substring to produce different keys")
	}
}

func TestComputeAnalysisKey_MatchesPreRefactorDigest(t *testing.T) {
	got := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", SystemPrompt: "sys", UserPromptTemplate: "tmpl"})
	want := "4cb31f140f7789067d939c2ec91ce1a41028c3bdc51df3c9b8466c30c0a62ab3"
	if got != want {
		t.Errorf("expected digest to match the pre-refactor field order, got %q want %q", got, want)
	}
}

func TestCache_AnalysisRoundTrip(t *testing.T) {
	c, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	key := ComputeAnalysisKey(AnalysisKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", SystemPrompt: "sys", UserPromptTemplate: "tmpl"})

	if _, found, err := c.Get(key); err != nil || found {
		t.Fatalf("expected cache miss before Put, found=%v err=%v", found, err)
	}

	want := &llm.AnalysisResult{Violation: true, Reasoning: "stub reasoning"}
	if err := c.Put(key, want); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, found, err := c.Get(key)
	if err != nil || !found {
		t.Fatalf("expected cache hit after Put, found=%v err=%v", found, err)
	}
	if got.Violation != want.Violation || got.Reasoning != want.Reasoning {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestCache_SuggestionRoundTrip(t *testing.T) {
	c, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	key := ComputeSuggestionKey(SuggestionKeyInput{ModelName: "gpt-4", ADRContent: "adr", FileContent: "code", Filename: "file.go", Reasoning: "reasoning", QuotedCode: "quoted", SuggestionSystemPrompt: "sys", SuggestionPromptTemplate: "tmpl"})

	if _, found, err := c.GetSuggestion(key); err != nil || found {
		t.Fatalf("expected cache miss before Put, found=%v err=%v", found, err)
	}

	if err := c.PutSuggestion(key, "move this to Go"); err != nil {
		t.Fatalf("PutSuggestion failed: %v", err)
	}

	got, found, err := c.GetSuggestion(key)
	if err != nil || !found {
		t.Fatalf("expected cache hit after Put, found=%v err=%v", found, err)
	}
	if got != "move this to Go" {
		t.Errorf("expected suggestion %q, got %q", "move this to Go", got)
	}
}

func TestCache_SuggestionDoesNotCollideWithAnalysisEntry(t *testing.T) {
	c, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	// Same key value used in both namespaces to prove they're stored separately.
	key := "shared-key"

	if err := c.Put(key, &llm.AnalysisResult{Violation: true, Reasoning: "stub reasoning"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if err := c.PutSuggestion(key, "a suggestion"); err != nil {
		t.Fatalf("PutSuggestion failed: %v", err)
	}

	res, found, err := c.Get(key)
	if err != nil || !found || res.Reasoning != "stub reasoning" {
		t.Fatalf("expected analysis entry to be unaffected by suggestion write, got res=%+v found=%v err=%v", res, found, err)
	}

	suggestion, found, err := c.GetSuggestion(key)
	if err != nil || !found || suggestion != "a suggestion" {
		t.Fatalf("expected suggestion entry to be unaffected by analysis write, got suggestion=%q found=%v err=%v", suggestion, found, err)
	}
}
