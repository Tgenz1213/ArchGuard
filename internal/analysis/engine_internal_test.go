package analysis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
)

type MockTruncationProvider struct {
	Content string
}

func (m *MockTruncationProvider) GetFiles() ([]string, error)            { return []string{"test.go"}, nil }
func (m *MockTruncationProvider) GetContent(path string) (string, error) { return m.Content, nil }
func (m *MockTruncationProvider) GetDiff(path string) (string, error)    { return "", nil }

func TestFetchContext_SmartTruncation(t *testing.T) {
	// A long string with newlines.
	// We want enough tokens so that MaxTokens=5 cuts it off.
	// "Line1" -> ~2 tokens
	// "\n" -> 1 token
	// "Line2" -> ~2 tokens
	// "Line3"
	longContent := "Line1\nLine2\nLine3"

	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: 4,
			Model:     "gpt-3.5-turbo",
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: longContent},
		Provider: llm.NewOpenAIProvider("unused-key", "gpt-3.5-turbo", "unused-embed-model"),
	}

	content, mode, err := engine.fetchContext(context.Background(), "test.go")
	if err != nil {
		t.Fatalf("fetchContext failed: %v", err)
	}

	if mode != "truncated" {
		t.Errorf("expected mode truncated, got %s", mode)
	}

	t.Logf("Truncated content: %q", content)

	// We expect the content to be rolled back to the newline.
	expected := "Line1\n"
	if content != expected {
		t.Errorf("Expected content to be rolled back to newline (%q), but got %q", expected, content)
	}
}

// TestFetchContext_NonOpenAI_UsesProviderTokenCount asserts truncation for
// a non-OpenAI provider is driven by that provider's own CountTokens, not
// by tiktoken's cl100k_base fallback. The mock provider here counts
// tokens completely differently from tiktoken (1 token per 2 bytes, vs.
// cl100k_base's real BPE), so if the engine were still silently using
// tiktoken under the hood, this test's truncation boundary would land
// somewhere else and the assertion below would fail.
func TestFetchContext_NonOpenAI_UsesProviderTokenCount(t *testing.T) {
	// 20 bytes: "AAAAAAAAAA\nBBBBBBBBB" -> mock counts 1 token per 2 bytes = 10 tokens total.
	content := "AAAAAAAAAA\nBBBBBBBBB"

	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: 5, // half of the mock's 10-token total -> must truncate
			Model:     "llama3.2",
			Provider:  "ollama",
		},
	}

	mockProvider := &llm.MockProvider{
		CountTokensFunc: func(ctx context.Context, text string) (int, error) {
			return len(text) / 2, nil
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: content},
		Provider: mockProvider,
	}

	got, mode, err := engine.fetchContext(context.Background(), "test.go")
	if err != nil {
		t.Fatalf("fetchContext failed: %v", err)
	}
	if mode != "truncated" {
		t.Fatalf("expected mode truncated, got %s", mode)
	}
	// MaxTokens=5 * 2 bytes/token = 10 bytes -> "AAAAAAAAAA" (exactly 10
	// bytes, no newline yet) -> no preceding newline to roll back to, so
	// content is returned as-is at the 10-byte boundary.
	expected := "AAAAAAAAAA"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

// TestFetchContext_CountTokensError_PropagatesLoudly asserts that if the
// provider can't produce a token count, fetchContext returns an error
// instead of silently falling back to a length-based heuristic.
func TestFetchContext_CountTokensError_PropagatesLoudly(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: 100,
			Model:     "some-model",
			Provider:  "ollama",
		},
	}

	mockProvider := &llm.MockProvider{
		CountTokensFunc: func(ctx context.Context, text string) (int, error) {
			return 0, errors.New("model not found on server")
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: "some file content"},
		Provider: mockProvider,
	}

	_, _, err := engine.fetchContext(context.Background(), "test.go")
	if err == nil {
		t.Fatal("expected fetchContext to return an error when CountTokens fails, got nil")
	}
}

// TestFetchContext_TruncationGuaranteesTokenBudget asserts that
// truncateToTokenLimit guarantees the returned content's token count (per
// Provider.CountTokens) never exceeds maxTokens, even when the
// whole-content bytesPerToken average is a poor predictor of local
// density.
//
// This mock provider counts only the last min(len(text), denseWindow)
// bytes as "expensive" (1 token/byte within that window); anything beyond
// that window is free. So any candidate that still reaches into the dense
// window costs ~denseWindow tokens almost no matter how much of the much
// larger cheap prefix is trimmed off. maxTokens is set to denseWindow-1, so
// the proportional-shrink ratio (maxTokens/denseWindow) is so close to 1
// that scaling the cut by that ratio alone would need thousands of
// iterations to work the cut down below the dense window -- far more than
// a small bounded number of proportional attempts allows. The guarantee
// only holds if a halving fallback kicks in afterward and keeps shrinking,
// unconditionally, until the measured count actually fits.
func TestFetchContext_TruncationGuaranteesTokenBudget(t *testing.T) {
	const denseWindow = 1000
	const maxTokens = denseWindow - 1 // 999: proportional ratio ~0.999, deliberately near 1

	content := strings.Repeat("x", 100_000) // 100x the dense window; all cheap bytes

	cfg := &config.Config{
		LLM: config.LLMConfig{
			MaxTokens: maxTokens,
			Model:     "some-model",
			Provider:  "ollama",
		},
	}

	lastLen := -1
	mockProvider := &llm.MockProvider{
		CountTokensFunc: func(ctx context.Context, text string) (int, error) {
			if len(text) == lastLen {
				t.Errorf("CountTokens called twice with the same-length candidate (%d bytes) -- redundant call", len(text))
			}
			lastLen = len(text)

			if len(text) > denseWindow {
				return denseWindow, nil
			}
			return len(text), nil
		},
	}

	engine := &Engine{
		Config:   cfg,
		Content:  &MockTruncationProvider{Content: content},
		Provider: mockProvider,
	}

	got, mode, err := engine.fetchContext(context.Background(), "test.go")
	if err != nil {
		t.Fatalf("fetchContext failed: %v", err)
	}
	if mode != "truncated" {
		t.Fatalf("expected mode truncated, got %s", mode)
	}

	lastLen = -1 // this verification call is expected to re-measure the final candidate
	finalTokens, err := mockProvider.CountTokens(context.Background(), got)
	if err != nil {
		t.Fatalf("CountTokens failed: %v", err)
	}
	if finalTokens > maxTokens {
		t.Errorf("truncateToTokenLimit did not honor the token budget: got %d tokens (content length %d bytes), want <= %d", finalTokens, len(got), maxTokens)
	}
}

// diffHeaderLines returns the diff --git/index/---/+++ preamble lines git
// emits before a file's first hunk, so test fixtures below only need to
// spell out the part that's actually interesting: the hunk body.
func diffHeaderLines(file string) []string {
	return []string{
		"diff --git a/" + file + " b/" + file,
		"index 1234567..89abcde 100644",
		"--- a/" + file,
		"+++ b/" + file,
	}
}

func TestStripDiffMetadata(t *testing.T) {
	singleHunkDiff := strings.Join(append(diffHeaderLines("foo.go"), []string{
		"@@ -1,4 +1,5 @@",
		" package foo",
		" ",
		"-func old() {}",
		"+func new() {}",
		"+func another() {}",
	}...), "\n")
	wantSingleHunk := strings.Join([]string{
		"package foo",
		"",
		"func old() {}",
		"func new() {}",
		"func another() {}",
	}, "\n")

	multiHunkDiff := strings.Join(append(diffHeaderLines("bar.go"), []string{
		"@@ -1,2 +1,2 @@",
		"-const A = 1",
		"+const A = 2",
		"@@ -10,2 +10,2 @@",
		"-const B = 1",
		"+const B = 2",
	}...), "\n")
	wantMultiHunk := strings.Join([]string{
		"const A = 1",
		"const A = 2",
		"const B = 1",
		"const B = 2",
	}, "\n")

	noNewlineDiff := strings.Join(append(diffHeaderLines("baz.go"), []string{
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
		"\\ No newline at end of file",
	}...), "\n")
	wantNoNewline := strings.Join([]string{"old", "new"}, "\n")

	// A removed line whose real source content starts with "-- " (a SQL/
	// Lua/Haskell-style comment marker) becomes "--- ..." once git
	// prepends the '-' diff marker -- colliding with the "--- a/file"
	// header prefix. Regression test for that marker/header collision.
	markerCollisionDiff := strings.Join(append(diffHeaderLines("query.sql"), []string{
		"@@ -1,3 +1,4 @@",
		" SELECT 1;",
		"--- old comment",
		"+SELECT 2;",
		" trailing context;",
	}...), "\n")
	wantMarkerCollision := strings.Join([]string{
		"SELECT 1;",
		"-- old comment",
		"SELECT 2;",
		"trailing context;",
	}, "\n")

	// ArchGuard never produces multi-file diffs (internal/git always
	// diffs a single path), but stripDiffMetadata defends against one
	// anyway: a second file's preamble must reset out of hunk mode, not
	// be corrupted by 1-byte stripping like ordinary hunk content.
	multiFileDiff := strings.Join(append(
		append(diffHeaderLines("file1.go"), "@@ -1,1 +1,1 @@", "+func f1() {}"),
		append(diffHeaderLines("file2.go"), "@@ -1,1 +1,1 @@", "+func f2() {}")...,
	), "\n")
	wantMultiFile := strings.Join([]string{"func f1() {}", "func f2() {}"}, "\n")

	plainFileContent := strings.Join([]string{
		"package foo",
		"",
		"func indented() {",
		"    return",
		"}",
	}, "\n")

	// A doc file that happens to contain a bare "@@..." line (e.g.
	// documenting this very stripping behavior with an example hunk
	// header) but no real "diff --git" header. Must not be misclassified
	// as a diff -- stripping this would corrupt every following line by
	// chopping off its first character.
	docWithBareHunkLookalike := strings.Join([]string{
		"# Example",
		"@@ -1,3 +1,4 @@",
		"    indented content that must survive untouched",
	}, "\n")

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"single hunk strips headers and markers", singleHunkDiff, wantSingleHunk},
		{"multiple hunks strip independently", multiHunkDiff, wantMultiHunk},
		{"no-newline marker line is dropped", noNewlineDiff, wantNoNewline},
		{"removed line starting with -- doesn't collide with the --- header", markerCollisionDiff, wantMarkerCollision},
		{"multi-file diff's second preamble resets out of hunk mode", multiFileDiff, wantMultiFile},
		{"non-diff content passed through unchanged", plainFileContent, plainFileContent},
		{"empty string unchanged", "", ""},
		{"bare hunk-header lookalike without diff --git is not stripped", docWithBareHunkLookalike, docWithBareHunkLookalike},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripDiffMetadata(c.input); got != c.want {
				t.Errorf("stripDiffMetadata(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestIsUnifiedDiff(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want bool
	}{
		{
			"real diff with multi-line hunk counts",
			"diff --git a/foo.go b/foo.go\nindex 111..222 100644\n--- a/foo.go\n+++ b/foo.go\n@@ -1,4 +1,5 @@\n content",
			true,
		},
		{
			"real diff with single-line hunk (no comma count)",
			"diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new",
			true,
		},
		{"bare @@ line without diff --git header", "# Example\n@@ -1,3 +1,4 @@\ncontent", false},
		{"diff --git header without a hunk header", "diff --git a/foo.go b/foo.go\nrenamed", false},
		{"plain content with neither", "package foo\n\nfunc x() {}", false},
		{"empty string", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isUnifiedDiff(c.s); got != c.want {
				t.Errorf("isUnifiedDiff(%q) = %v, want %v", c.s, got, c.want)
			}
		})
	}
}

func TestShouldExclude_RecursiveTestPattern(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.Analysis{
			ExcludePatterns: []string{"**/*_test.go", "vendor/**"},
		},
	}
	engine := &Engine{Config: cfg}

	cases := []struct {
		path string
		want bool
	}{
		{"foo_test.go", true},
		{"internal/analysis/glob_test.go", true}, // regression: previously only matched exactly 2 path segments deep
		{"internal/analysis/glob.go", false},
		{"vendor/pkg/sub/file.go", true},
	}

	for _, c := range cases {
		if got := engine.shouldExclude(c.path); got != c.want {
			t.Errorf("shouldExclude(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	cases := []struct {
		name  string
		s     string
		limit int
		want  string
	}{
		{"ascii under limit unchanged", "hello", 10, "hello"},
		{"ascii exact limit unchanged", "hello", 5, "hello"},
		{"ascii over limit cuts at byte boundary", "hello world", 5, "hello"},
		{"2-byte rune (é) split backs up to rune start", "café", 4, "caf"},
		{"3-byte rune (中) split backs up to rune start", "ab中cd", 4, "ab"},
		{"4-byte rune (😀) split backs up to rune start", "hi😀jk", 4, "hi"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncateRuneSafe(c.s, c.limit)
			if got != c.want {
				t.Errorf("truncateRuneSafe(%q, %d) = %q, want %q", c.s, c.limit, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateRuneSafe(%q, %d) = %q, not valid UTF-8", c.s, c.limit, got)
			}
			if len(got) > c.limit {
				t.Errorf("truncateRuneSafe(%q, %d) = %q, exceeds limit (%d bytes)", c.s, c.limit, got, len(got))
			}
		})
	}
}

func TestRollBackToNewline(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want string
	}{
		{"no newline returns unchanged", "hello", "hello"},
		{"trailing partial line rolled back", "line1\nline2", "line1\n"},
		{"already ends at newline unchanged", "line1\n", "line1\n"},
		{"multiple newlines rolls back to last", "a\nb\nc", "a\nb\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rollBackToNewline(c.s); got != c.want {
				t.Errorf("rollBackToNewline(%q) = %q, want %q", c.s, got, c.want)
			}
		})
	}
}

// TestEmbeddingTruncation_MultiByteBoundary asserts the diffForEmbedding
// truncation site (6000-byte limit) never splits a multi-byte UTF-8 rune,
// using content engineered so a raw byte cut at 6000 would land mid-rune.
func TestEmbeddingTruncation_MultiByteBoundary(t *testing.T) {
	const limit = 6000
	prefix := strings.Repeat("x", limit-2) + "\n"
	content := prefix + "中文内容" + strings.Repeat("y", 100)

	got := rollBackToNewline(truncateRuneSafe(content, limit))

	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
	if len(got) > limit {
		t.Fatalf("result exceeds limit: %d bytes > %d", len(got), limit)
	}
	if got != prefix {
		t.Errorf("expected rollback to prefix ending at newline (%q), got %q", prefix, got)
	}
}

// TestIgnoreHeaderTruncation_MultiByteBoundary asserts the archguard-ignore
// header truncation site (2000-byte limit) never splits a multi-byte UTF-8
// rune, using content engineered so a raw byte cut at 2000 would land
// mid-rune.
func TestIgnoreHeaderTruncation_MultiByteBoundary(t *testing.T) {
	const limit = 2000
	content := strings.Repeat("x", limit-2) + "日本語" + strings.Repeat("y", 100)

	got := truncateRuneSafe(content, limit)

	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
	if len(got) > limit {
		t.Fatalf("result exceeds limit: %d bytes > %d", len(got), limit)
	}
}
