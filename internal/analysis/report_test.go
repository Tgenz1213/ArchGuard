package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestReport_CITruncatedFileIsListedAsSkipped(t *testing.T) {
	chatCalls := 0
	provider := &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) {
			chatCalls++

			return `{"violation": true, "reasoning": "r", "quoted_code": "import python_library"}`, nil
		},
	}

	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{
		ID:        "0001",
		Title:     "Use Golang",
		Status:    "Accepted",
		Content:   "All services must be Go.",
		Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
	}}

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
		LLM:         config.LLMConfig{MaxTokens: 5},
	}
	bigContent := strings.Repeat("x", 200) + "\nimport python_library\n"
	content := &fallbackOnlyContentProvider{files: map[string]string{"service.py": bigContent}}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.CI = true
	engine.Cache = nil

	captureStderr(t, func() { runEngine(t, engine, false) })

	if chatCalls != 0 {
		t.Errorf("a file too large to analyze in CI mode reached the LLM %d time(s)", chatCalls)
	}

	if skipped := engine.Report().SkippedFiles; len(skipped) != 1 || skipped[0].File != "service.py" {
		t.Errorf("expected service.py listed as skipped, got %+v", skipped)
	}
}

func baselineEngine(t *testing.T, chatResponse string, content analysis.ContentProvider, maxTokens int) *analysis.Engine {
	t.Helper()

	provider := &inference.MockProvider{
		ChatFunc: func(context.Context, string, string) (string, error) { return chatResponse, nil },
	}
	store := index.NewLocalStore(5)
	store.ADRs = []index.ADR{{
		ID:        "0001",
		Title:     "Use Golang",
		Status:    "Accepted",
		Content:   "All services must be Go.",
		Embedding: func() []float32 { v := make([]float32, 1536); v[0] = 1.0; return v }(),
	}}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
		LLM:         config.LLMConfig{MaxTokens: maxTokens},
	}

	engine := analysis.NewEngine(cfg, store, provider, provider, content)
	engine.Cache = nil
	engine.UpdateBaseline = true
	engine.BaselineReason = "accepted-debt"

	captureStderr(t, func() { runEngine(t, engine, false) })

	return engine
}

func TestBaselineReport_RecordsEntryWithLineAndReason(t *testing.T) {
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}
	engine := baselineEngine(t, `{"violation": true, "reasoning": "r", "quoted_code": "import python_library"}`, content, 0)

	report := engine.BaselineReport("archguard-baseline.json")

	want := output.RecordedEntry{File: "service.py", ADRID: "0001", Title: "Use Golang", Line: 1, Reason: "accepted-debt"}
	if len(report.Recorded) != 1 || report.Recorded[0] != want {
		t.Errorf("Recorded = %+v, want [%+v]", report.Recorded, want)
	}

	if len(report.Unrecorded) != 0 || report.Path != "archguard-baseline.json" {
		t.Errorf("unexpected unrecorded entries or path: %+v", report)
	}
}

func TestBaselineReport_ListsViolationsItCouldNotRecordWithWhy(t *testing.T) {
	content := &MockContentProvider{Files: map[string]string{"service.py": "import python_library\n"}}
	engine := baselineEngine(t, `{"violation": true, "reasoning": "r", "quoted_code": "text that is not in the file"}`, content, 0)

	report := engine.BaselineReport("archguard-baseline.json")

	if len(report.Recorded) != 0 {
		t.Errorf("recorded an entry whose quote is not in the file: %+v", report.Recorded)
	}

	if len(report.Unrecorded) != 1 || report.Unrecorded[0].File != "service.py" || report.Unrecorded[0].ADRID != "0001" || report.Unrecorded[0].Reason == "" {
		t.Errorf("Unrecorded = %+v, want service.py / ADR 0001 with a reason", report.Unrecorded)
	}
}

func TestBaselineReport_ListsTruncatedFilesAsPartlyChecked(t *testing.T) {
	bigContent := strings.Repeat("x", 200) + "\nimport python_library\n"
	content := &fallbackOnlyContentProvider{files: map[string]string{"service.py": bigContent}}
	engine := baselineEngine(t, `{"violation": true, "reasoning": "r", "quoted_code": "import python_library"}`, content, 5)

	report := engine.BaselineReport("archguard-baseline.json")

	if len(report.PartialFiles) != 1 || report.PartialFiles[0].File != "service.py" {
		t.Errorf("PartialFiles = %+v, want service.py", report.PartialFiles)
	}
}
