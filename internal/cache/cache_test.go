package cache

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/llm"
)

func TestComputeSuggestionKey_StableForSameInputs(t *testing.T) {
	a := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "sys", "tmpl")
	b := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "sys", "tmpl")
	if a != b {
		t.Errorf("expected identical inputs to produce the same key, got %q and %q", a, b)
	}
}

func TestComputeSuggestionKey_ChangesWithSuggestionPrompt(t *testing.T) {
	a := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "old system prompt", "old template")
	b := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "new system prompt", "old template")
	if a == b {
		t.Error("expected a changed suggestion system prompt to change the key")
	}

	c := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "old system prompt", "new template")
	if a == c {
		t.Error("expected a changed suggestion prompt template to change the key")
	}
}

func TestComputeSuggestionKey_ChangesWithFilename(t *testing.T) {
	a := ComputeSuggestionKey("gpt-4", "adr", "code", "old/path.go", "reasoning", "quoted", "sys", "tmpl")
	b := ComputeSuggestionKey("gpt-4", "adr", "code", "new/path.go", "reasoning", "quoted", "sys", "tmpl")
	if a == b {
		t.Error("expected identical content under a different file path to change the key, since the rendered suggestion prompt includes the file path")
	}
}

func TestComputeSuggestionKey_NoAmbiguousFieldBoundaries(t *testing.T) {
	a := ComputeSuggestionKey("m", "a||b", "c", "f", "r", "q", "s", "t")
	b := ComputeSuggestionKey("m", "a", "b||c", "f", "r", "q", "s", "t")
	if a == b {
		t.Error("expected differently-split fields around a literal delimiter-like substring to produce different keys")
	}
}

func TestComputeSuggestionKey_IndependentOfAnalysisKey(t *testing.T) {
	analysisKey := ComputeAnalysisKey("gpt-4", "adr", "code", "judgment system prompt", "judgment template")
	suggestionKey := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "suggestion system prompt", "suggestion template")
	if analysisKey == suggestionKey {
		t.Error("expected analysis and suggestion keys to live in independent namespaces")
	}
}

func TestCache_SuggestionRoundTrip(t *testing.T) {
	c, err := NewCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}

	key := ComputeSuggestionKey("gpt-4", "adr", "code", "file.go", "reasoning", "quoted", "sys", "tmpl")

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
