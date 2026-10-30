package analysis_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

const (
	changeSampleHunks    = "@@ -1,5 +1,6 @@\n package a\n-var old = 1\n+var added = 1\n+var alsoAdded = 2\n \n func keep() {}\n"
	changeSamplePreamble = "diff --git a/service.py b/service.py\nindex 111..222 100644\n--- a/service.py\n+++ b/service.py\n"
)

type changeRunOptions struct {
	whole          string
	diff           string
	quote          string
	judgeChange    bool
	updateBaseline bool
	maxTokens      int
	cache          *cache.Cache
}

type changeRun struct {
	engine    *analysis.Engine
	chatCalls int
}

func runChange(t *testing.T, opts changeRunOptions) *changeRun {
	t.Helper()

	run := &changeRun{}
	vector := make([]float32, 1536)
	vector[0] = 1.0

	provider := &inference.MockProvider{
		EmbedFunc: func(context.Context, string, inference.EmbeddingTaskType) ([]float32, error) {
			return vector, nil
		},
		ChatFunc: func(context.Context, string, string) (string, error) {
			run.chatCalls++

			return fmt.Sprintf(`{"violation": true, "reasoning": "r", "quoted_code": %q}`, opts.quote), nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{ID: "0001", Title: "Use Golang", Status: "Accepted", Content: "All services must be Go.", Embedding: vector}}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
		LLM:         config.LLMConfig{MaxTokens: opts.maxTokens},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, &diffCapableContentProvider{content: opts.whole, diff: opts.diff})
	engine.Cache = opts.cache
	engine.JudgeChange = opts.judgeChange
	engine.UpdateBaseline = opts.updateBaseline
	run.engine = engine

	var drift *analysis.DriftDetectedError
	if err := engine.Run(t.Context()); err != nil && !errors.As(err, &drift) {
		t.Fatalf("Run failed: %v", err)
	}

	return run
}

func (run *changeRun) findings() []analysis.Violation {
	return slices.Concat(run.engine.CollectedViolations, run.engine.UnverifiedFindings)
}

func (run *changeRun) verified() bool {
	return len(run.engine.UnverifiedFindings) == 0
}

func TestChange_BinaryDiffIsSkippedSilently(t *testing.T) {
	run := runChange(t, changeRunOptions{whole: "x", diff: "diff --git a/x b/x\nBinary files a/x and b/x differ\n", quote: "x", judgeChange: true})

	if run.chatCalls != 0 || len(run.engine.SkippedFiles) != 0 || len(run.findings()) != 0 {
		t.Fatalf("chat calls = %d, skipped = %+v, findings = %+v; want nothing judged, nothing recorded", run.chatCalls, run.engine.SkippedFiles, run.findings())
	}
}

func TestChange_Findings(t *testing.T) {
	tests := []struct {
		name         string
		quote        string
		wantFindings int
		wantLine     int
		wantVerified bool
	}{
		{"a quote in unchanged context is dropped", "func keep() {}", 0, 0, false},
		{"a quote in an added line keeps its new-file line", "var added = 1", 1, 2, true},
		{"a quote in a removed line keeps the line it was removed from", "var old = 1", 1, 2, true},
		{"a quote found nowhere stays unverified", "something the model invented", 1, 0, false},
		{"an empty quote is unverified, not verified", "", 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runChange(t, changeRunOptions{whole: "ignored", diff: changeSamplePreamble + changeSampleHunks, quote: tt.quote, judgeChange: true})

			found := run.findings()
			if len(found) != tt.wantFindings {
				t.Fatalf("findings = %+v, want %d", found, tt.wantFindings)
			}

			if tt.wantFindings == 0 {
				return
			}

			if found[0].Line != tt.wantLine {
				t.Errorf("line = %d, want %d", found[0].Line, tt.wantLine)
			}

			if got := run.verified(); got != tt.wantVerified {
				t.Errorf("verified = %v, want %v", got, tt.wantVerified)
			}
		})
	}
}

func TestRun_UnwrapsMarkdownAroundQuote(t *testing.T) {
	const file = "package a\nimport \"os\"\nvar q = `select 1`\n"

	tests := []struct {
		name         string
		quote        string
		wantQuote    string
		wantLine     int
		wantVerified bool
	}{
		{"a fenced block with a language tag", "```go\nimport \"os\"\n```", `import "os"`, 2, true},
		{"a fenced block without a language tag", "```\nimport \"os\"\n```", `import "os"`, 2, true},
		{"a single-line fenced block", "```import \"os\"```", `import "os"`, 2, true},
		{"inline backticks", "`import \"os\"`", `import "os"`, 2, true},
		{"backticks that belong to the code", "`select 1`", "`select 1`", 3, true},
		{"an unwrapped quote", `import "os"`, `import "os"`, 2, true},
		{"a wrapped quote that is not in the file keeps its markdown", "```go\nimport \"fmt\"\n```", "```go\nimport \"fmt\"\n```", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runChange(t, changeRunOptions{whole: file, quote: tt.quote})

			found := run.findings()
			if len(found) != 1 {
				t.Fatalf("findings = %+v, want 1", found)
			}

			if found[0].QuotedCode != tt.wantQuote || found[0].Line != tt.wantLine {
				t.Errorf("quote, line = %q, %d; want %q, %d", found[0].QuotedCode, found[0].Line, tt.wantQuote, tt.wantLine)
			}

			if got := run.verified(); got != tt.wantVerified {
				t.Errorf("verified = %v, want %v", got, tt.wantVerified)
			}
		})
	}
}

func TestRun_UnwrapsMarkdownAroundQuoteUnderSince(t *testing.T) {
	run := runChange(t, changeRunOptions{whole: "ignored", diff: changeSamplePreamble + changeSampleHunks, quote: "```go\nvar added = 1\n```", judgeChange: true})

	found := run.engine.CollectedViolations
	if len(found) != 1 || found[0].QuotedCode != "var added = 1" || found[0].Line != 2 {
		t.Fatalf("findings = %+v, want one at line 2 quoting the unwrapped line", found)
	}
}

func TestRun_UnwrapsMarkdownAroundCachedQuote(t *testing.T) {
	shared, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}

	const file = "package a\nimport \"os\"\n"

	runChange(t, changeRunOptions{whole: file, quote: "```go\nimport \"os\"\n```", cache: shared})
	second := runChange(t, changeRunOptions{whole: file, quote: "unused: the cached result answers", cache: shared})

	found := second.engine.CollectedViolations
	if second.chatCalls != 0 || len(found) != 1 || found[0].QuotedCode != `import "os"` {
		t.Fatalf("chat calls = %d, findings = %+v; want the cached fenced quote unwrapped", second.chatCalls, found)
	}
}

func TestRun_UpdateBaselineRecordsUnwrappedQuote(t *testing.T) {
	run := runChange(t, changeRunOptions{whole: "package a\nimport \"os\"\n", quote: "`import \"os\"`", updateBaseline: true})

	entries := run.engine.CollectedBaseline.Entries
	if len(entries) != 1 || entries[0].QuotedCode != `import "os"` {
		t.Fatalf("baseline entries = %+v, want one recording the unwrapped quote", entries)
	}
}

func TestRun_ReportsFileLinesForAFileAnalyzedThroughItsDiff(t *testing.T) {
	whole := strings.Repeat("// filler\n", 197) + "func a() {}\nfunc b() {}\nvar leak = password\n"
	diff := changeSamplePreamble + "@@ -198,3 +198,3 @@\n func a() {}\n-func old() {}\n+func b() {}\n+var leak = password\n"

	tests := []struct {
		name         string
		quote        string
		wantLine     int
		wantVerified bool
	}{
		{"an added line", "var leak = password", 200, true},
		{"an unchanged line", "func a() {}", 198, true},
		{"lines across an unchanged and an added line", "func a() {}\nfunc b() {}", 198, true},
		{"a removed line is not in the file", "func old() {}", 0, false},
		{"a quote found nowhere", "something the model invented", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runChange(t, changeRunOptions{whole: whole, diff: diff, quote: tt.quote, maxTokens: 10})

			found := run.findings()
			if len(found) != 1 {
				t.Fatalf("findings = %+v, want 1", found)
			}

			if found[0].Line != tt.wantLine {
				t.Errorf("line = %d, want %d", found[0].Line, tt.wantLine)
			}

			if got := run.verified(); got != tt.wantVerified {
				t.Errorf("verified = %v, want %v", got, tt.wantVerified)
			}
		})
	}
}

func TestChange_CacheKeyDiffersFromWholeFileJudging(t *testing.T) {
	shared, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}

	runChange(t, changeRunOptions{whole: changeSampleHunks, quote: "var added = 1", cache: shared})
	second := runChange(t, changeRunOptions{whole: "ignored", diff: changeSamplePreamble + changeSampleHunks, quote: "var added = 1", judgeChange: true, cache: shared})

	if second.chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1: a --since run must not be served the whole-file verdict for identical text", second.chatCalls)
	}
}

func TestEngine_CountsJudgedFilesAndADRChecks(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := output.Coverage{FilesJudged: 1, ADRChecks: 2}
	if got := h.engine.Report().Coverage; got != want {
		t.Fatalf("coverage = %+v, want %+v", got, want)
	}
}

func TestEngine_CountsEveryFileWhenEveryCandidateIsBelowTheThreshold(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.Content.(*MockContentProvider).Files = map[string]string{"a.go": "package a", "b.go": "package b"}
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 0.4, "0002": 0.3}, nil), Min: stage.FixedMin(0.5), MaxKeep: 3}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := output.Coverage{FilesWithoutADR: 2, FilesBelowThreshold: 2}
	if got := h.engine.Report().Coverage; got != want {
		t.Fatalf("coverage = %+v, want %+v", got, want)
	}
}

func TestEngine_CountsAScreenedOutFileApartFromOneBelowTheThreshold(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.Stages = []stage.Stage{{Name: "screen", Screens: true, Scorer: scoresByID(map[string]float64{"0001": 0.1, "0002": 0.1}, nil), Min: stage.FixedMin(0.5)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := output.Coverage{FilesWithoutADR: 1, FilesScreenedOut: 1}
	if got := h.engine.Report().Coverage; got != want {
		t.Fatalf("coverage = %+v, want %+v", got, want)
	}
}

func TestEngine_CountsFilesWithNoScopeMatchAsWithoutADR(t *testing.T) {
	adr := scorerADR("0001", 1)
	adr.Scope = []string{"docs/**"}
	h := newScorerHarness(t, []index.ADR{adr}, "a.go", "package a")

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := output.Coverage{FilesWithoutADR: 1}
	if got := h.engine.Report().Coverage; got != want {
		t.Fatalf("coverage = %+v, want %+v", got, want)
	}
}

func TestEngine_DoesNotCountASkippedFileAsJudged(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")
	h.engine.Stages = []stage.Stage{{Scorer: scorerFunc(func(context.Context, stage.File, stage.Debug, []stage.Candidate) ([]float64, error) {
		return nil, errors.New("down")
	})}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := h.engine.Report()
	if len(report.SkippedFiles) != 1 || report.Coverage != (output.Coverage{}) {
		t.Fatalf("skipped = %+v, coverage = %+v; want the file skipped and nothing counted", report.SkippedFiles, report.Coverage)
	}
}

func TestEngine_CountsFilesAcrossARun(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")
	h.engine.Content.(*MockContentProvider).Files = map[string]string{"a.go": "package a", "b.go": "package b", "c.go": "package c"}
	h.engine.Stages = []stage.Stage{{Min: stage.FixedMin(0.5), Scorer: scorerFunc(func(_ context.Context, file stage.File, _ stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		score := 1.0
		if file.Path() == "c.go" {
			score = 0.1
		}

		scores := make([]float64, len(candidates))
		for i := range scores {
			scores[i] = score
		}

		return scores, nil
	})}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := output.Coverage{FilesJudged: 2, ADRChecks: 2, FilesWithoutADR: 1, FilesBelowThreshold: 1}
	if got := h.engine.Report().Coverage; got != want {
		t.Fatalf("coverage = %+v, want %+v", got, want)
	}
}

func TestEngine_DoesNotCountAFileWhoseEveryCheckFailedAsJudged(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")
	h.engine.Chat = &inference.MockProvider{ChatFunc: func(context.Context, string, string) (string, error) {
		return "", backoff.Permanent(errors.New("down"))
	}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := h.engine.Report()
	if len(report.FailedChecks) != 1 || report.Coverage != (output.Coverage{}) {
		t.Fatalf("failed checks = %+v, coverage = %+v; want one failed check and nothing counted", report.FailedChecks, report.Coverage)
	}
}

func TestEngine_DoesNotCountAPartlyCheckedFileWithNoCandidateAsWithoutADR(t *testing.T) {
	adr := scorerADR("0001", 1)
	adr.Scope = []string{"docs/**"}
	h := newScorerHarness(t, []index.ADR{adr}, "a.go", "package a")
	h.engine.Config.LLM.MaxTokens = 5
	h.engine.Content = &fallbackOnlyContentProvider{files: map[string]string{"a.go": strings.Repeat("x", 200)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := h.engine.Report()
	if len(report.PartialFiles) != 1 || report.Coverage != (output.Coverage{}) {
		t.Fatalf("partial = %+v, coverage = %+v; want the file partly checked and nothing counted", report.PartialFiles, report.Coverage)
	}
}

func TestEngine_CheckedNothing(t *testing.T) {
	failFor := func(files ...string) stage.Scorer {
		return scorerFunc(func(_ context.Context, file stage.File, _ stage.Debug, candidates []stage.Candidate) ([]float64, error) {
			if slices.Contains(files, file.Path()) {
				return nil, errors.New("down")
			}

			return slices.Repeat([]float64{1}, len(candidates)), nil
		})
	}
	failingChat := &inference.MockProvider{ChatFunc: func(context.Context, string, string) (string, error) {
		return "", backoff.Permanent(errors.New("down"))
	}}

	tests := []struct {
		name        string
		setup       func(h *scorerHarness)
		wantSkipped int
		want        bool
	}{
		{"every file's scoring failed", func(h *scorerHarness) { h.engine.Stages = []stage.Stage{{Scorer: failFor("a.go", "b.go")}} }, 2, true},
		{"one file judged beside a failed one", func(h *scorerHarness) { h.engine.Stages = []stage.Stage{{Scorer: failFor("b.go")}} }, 1, false},
		{"every ADR check failed", func(h *scorerHarness) { h.engine.Chat = failingChat }, 0, true},
		{"a file with no relevant ADR beside a failed one", func(h *scorerHarness) {
			h.engine.Store.(*index.LocalStore).ADRs[0].Scope = []string{"b.go"}
			h.engine.Stages = []stage.Stage{{Scorer: failFor("b.go")}}
		}, 1, true},
		{"files too large in CI mode are not errors", func(h *scorerHarness) {
			h.engine.Content = &fallbackOnlyContentProvider{files: map[string]string{"a.go": "package a", "b.go": "package b"}}
			h.engine.CI = true
			h.engine.Config.LLM.MaxTokens = 1
		}, 2, false},
		{"no relevant ADR and no error", func(h *scorerHarness) {
			h.engine.Store.(*index.LocalStore).ADRs[0].Scope = []string{"docs/**"}
		}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")
			h.engine.Content.(*MockContentProvider).Files = map[string]string{"a.go": "package a", "b.go": "package b"}
			h.engine.Out = output.Discard()
			tt.setup(h)

			if err := h.engine.Run(t.Context()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(h.engine.SkippedFiles) != tt.wantSkipped || h.engine.CheckedNothing != tt.want {
				t.Fatalf("skipped = %+v, CheckedNothing = %v; want %d skipped and %v (failed checks %+v, coverage %+v)", h.engine.SkippedFiles, h.engine.CheckedNothing, tt.wantSkipped, tt.want, h.engine.FailedChecks, h.engine.Coverage)
			}
		})
	}
}
