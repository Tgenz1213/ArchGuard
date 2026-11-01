package analysis_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type scorerFunc func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error)

func (f scorerFunc) Score(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
	return f(ctx, file, debug, candidates)
}

func scoresByID(scores map[string]float64, seen *[][]string) stage.Scorer {
	return scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		ids := make([]string, len(candidates))
		out := make([]float64, len(candidates))
		for i, c := range candidates {
			ids[i] = c.ADR.ID
			out[i] = scores[c.ADR.ID]
		}

		if seen != nil {
			*seen = append(*seen, ids)
		}

		return out, nil
	})
}

type scorerHarness struct {
	engine *analysis.Engine
	mu     sync.Mutex
	judged []string
	embeds int
}

func newScorerHarness(t *testing.T, adrs []index.ADR, file, fileContent string) *scorerHarness {
	t.Helper()
	h := &scorerHarness{}
	provider := &inference.MockProvider{
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, a := range adrs {
				if strings.Contains(user, a.Content) {
					h.judged = append(h.judged, a.ID)
				}
			}

			return `{"violation": false, "reasoning": "", "quoted_code": ""}`, nil
		},
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			h.mu.Lock()
			h.embeds++
			h.mu.Unlock()
			v := make([]float32, 4)
			v[0] = 1
			return v, nil
		},
	}

	store := index.NewLocalStore(1)
	store.ADRs = adrs

	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{MaxRelevantADRs: 3},
	}
	content := &MockContentProvider{Files: map[string]string{file: fileContent}}

	h.engine = newEngine(cfg, store, provider, provider, content)
	h.engine.Cache = nil
	return h
}

func scorerADR(id string, similarity float32) index.ADR {
	emb := make([]float32, 4)
	emb[0] = similarity
	emb[1] = 1 - similarity
	return index.ADR{ID: id, Title: "ADR " + id, Status: "Accepted", RelPath: id + ".md", Content: "rule-body-" + id, Embedding: emb}
}

func TestPipeline_ScoresAllCandidatesInOneCall(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1), scorerADR("0003", 1)}, "svc.go", "package svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 1, "0003": 1}, &calls)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 1 || len(calls[0]) != 3 {
		t.Fatalf("scorer calls = %v, want one call with all 3 candidates", calls)
	}

	if len(h.judged) != 3 {
		t.Fatalf("judged %v, want all 3", h.judged)
	}
}

func TestPipeline_OnlyWhatTheStageKeepsIsJudged(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1), scorerADR("0003", 1), scorerADR("0004", 1)}, "svc.go", "package svc")

	h.engine.Stages = []stage.Stage{{
		Scorer:  scoresByID(map[string]float64{"0001": 0.9, "0002": 0.2, "0003": 0.7, "0004": 0.8}, nil),
		Min:     stage.FixedMin(0.5),
		MaxKeep: 2,
	}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Join(h.judged, ",") != "0001,0004" {
		t.Fatalf("judged %v, want the two best scorers above the minimum, best first (0001,0004)", h.judged)
	}
}

func TestPipeline_StagesRunInOrderOverSurvivors(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1), scorerADR("0003", 1)}, "svc.go", "package svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{
		{Scorer: scoresByID(map[string]float64{"0001": 0.9, "0002": 0.8, "0003": 0.1}, &calls), Min: stage.FixedMin(0.5)},
		{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 0}, &calls), Min: stage.FixedMin(0.5)},
	}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 2 || len(calls[1]) != 2 {
		t.Fatalf("scorer calls = %v, want the second stage to see only the first stage's 2 survivors", calls)
	}

	if strings.Join(h.judged, ",") != "0001" {
		t.Fatalf("judged %v, want only 0001", h.judged)
	}
}

func TestPipeline_WithoutCosineMakesNoEmbeddingCalls(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "svc.go", "package svc")
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1}, nil)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.embeds != 0 {
		t.Fatalf("made %d embedding calls with no cosine ranker, want 0", h.embeds)
	}

	if len(h.judged) != 1 {
		t.Fatalf("judged %v, want the one candidate judged", h.judged)
	}
}

func TestPipeline_OnlyScopeMatchedADRsAreCandidates(t *testing.T) {
	inScope := scorerADR("0001", 1)
	inScope.Scope = index.ScopePatterns{"**/*.go"}
	outOfScope := scorerADR("0002", 1)
	outOfScope.Scope = index.ScopePatterns{"**/*.py"}
	h := newScorerHarness(t, []index.ADR{inScope, outOfScope}, "svc.go", "package svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 1}, &calls)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != "0001" {
		t.Fatalf("scorer calls = %v, want only the in-scope ADR 0001", calls)
	}
}

func TestPipeline_SuppressedADRsNeverReachScorerOrLLM(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "svc.go", "// archguard-ignore: 0001\npackage svc")

	var calls [][]string
	h.engine.Stages = []stage.Stage{{Scorer: scoresByID(map[string]float64{"0001": 1, "0002": 1}, &calls)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != "0002" {
		t.Fatalf("scorer calls = %v, want only the unsuppressed ADR 0002", calls)
	}

	if len(h.judged) != 1 || h.judged[0] != "0002" {
		t.Fatalf("judged %v, want only ADR 0002", h.judged)
	}
}

func runRankWithOnError(t *testing.T, onError string, embed inference.Embedder) *scorerHarness {
	t.Helper()
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "good.go", "package good")
	h.engine.Content.(*MockContentProvider).Files["bad.go"] = "package BAD"
	h.engine.Out = output.Discard()
	rank := config.Node{Name: "rank", Job: config.JobRank, With: config.RunnerCosine, TopK: 3, OnError: onError}

	h.engine.Stages = analysis.BuildPipeline([]config.Node{rank}, h.engine.Store, embed, nil)
	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return h
}

func embedFailingOnBAD() inference.Embedder {
	return &inference.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task inference.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "BAD") {
				return nil, errors.New("embedding service down")
			}

			return []float32{1, 0, 0, 0}, nil
		},
	}
}

func TestPipeline_OnErrorSkipAndDefaultSkipTheFile(t *testing.T) {
	for _, onError := range []string{"", config.OnErrorSkip} {
		t.Run("on_error="+onError, func(t *testing.T) {
			h := runRankWithOnError(t, onError, embedFailingOnBAD())
			if len(h.engine.SkippedFiles) != 1 || len(h.engine.StageFailures) != 0 {
				t.Fatalf("SkippedFiles = %d, StageFailures = %v; want the file skipped and no failures", len(h.engine.SkippedFiles), h.engine.StageFailures)
			}

			if skipped := h.engine.SkippedFiles[0]; skipped.File != "bad.go" || !strings.Contains(skipped.Reason, "embedding service down") {
				t.Errorf("SkippedFiles[0] = %+v, want bad.go skipped for the embedding error", skipped)
			}

			if strings.Join(h.judged, ",") != "0001" {
				t.Errorf("judged %v, want the healthy file still judged", h.judged)
			}
		})
	}
}

func TestPipeline_OnErrorFailUnavailable(t *testing.T) {
	harness := runRankWithOnError(t, config.OnErrorFail, embedFailingOnBAD())
	if len(harness.engine.SkippedFiles) != 0 || len(harness.engine.StageFailures) != 1 {
		t.Fatalf("SkippedFiles = %d, StageFailures = %v; want one failure and no skips", len(harness.engine.SkippedFiles), harness.engine.StageFailures)
	}

	failure := harness.engine.StageFailures[0]
	if failure.Stage != "rank" || failure.File != "bad.go" || failure.Kind != stage.KindUnavailable || !strings.Contains(failure.Error, "embedding service down") {
		t.Errorf("failure = %+v", failure)
	}

	if strings.Join(harness.judged, ",") != "0001" {
		t.Errorf("judged %v, want the healthy file still judged", harness.judged)
	}
}

func TestPipeline_OnErrorFailPreconditionNotMet(t *testing.T) {
	harness := runRankWithOnError(t, config.OnErrorFail, nil)
	if len(harness.engine.StageFailures) != 2 || len(harness.judged) != 0 {
		t.Fatalf("StageFailures = %v, judged = %v; want both files failed and nothing judged", harness.engine.StageFailures, harness.judged)
	}

	for _, failure := range harness.engine.StageFailures {
		if failure.Stage != "rank" || failure.Kind != stage.KindPreconditionNotMet {
			t.Errorf("failure = %+v, want a rank precondition failure", failure)
		}
	}
}

func TestPipeline_OnErrorSkipPreconditionSkipsFiles(t *testing.T) {
	h := runRankWithOnError(t, config.OnErrorSkip, nil)
	if len(h.engine.SkippedFiles) != 2 || len(h.engine.StageFailures) != 0 {
		t.Fatalf("SkippedFiles = %d, StageFailures = %v; want both files skipped", len(h.engine.SkippedFiles), h.engine.StageFailures)
	}
}

func failingScorer(kind stage.Kind) stage.Scorer {
	return scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		return nil, &stage.Error{Action: "scoring candidates", Kind: kind, Err: errors.New("boom")}
	})
}

func TestPipeline_OnErrorFailStopsRemainingStagesForThatFileOnly(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "good.go", "package good")
	h.engine.Content.(*MockContentProvider).Files["bad.go"] = "package BAD"

	var mu sync.Mutex
	var secondStageFiles []string
	first := scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		if file.Path() == "bad.go" {
			return nil, errors.New("boom")
		}

		return make([]float64, len(candidates)), nil
	})
	second := scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		mu.Lock()
		secondStageFiles = append(secondStageFiles, file.Path())
		mu.Unlock()
		return make([]float64, len(candidates)), nil
	})
	h.engine.Stages = []stage.Stage{
		{Name: "rank", Scorer: first, FailOnError: true},
		{Name: "rerank", Scorer: second},
	}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.engine.StageFailures) != 1 || h.engine.StageFailures[0].File != "bad.go" {
		t.Fatalf("StageFailures = %v, want one failure for bad.go", h.engine.StageFailures)
	}

	if strings.Join(secondStageFiles, ",") != "good.go" {
		t.Errorf("second stage ran for %v, want only good.go", secondStageFiles)
	}
}

func TestPipeline_StageFailuresAreSortedByFile(t *testing.T) {
	harness := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "m.go", "package m")
	files := harness.engine.Content.(*MockContentProvider).Files
	files["z.go"] = "package z"
	files["a.go"] = "package a"
	harness.engine.Config.Analysis.MaxConcurrency = 3
	harness.engine.Stages = []stage.Stage{{Name: "rank", Scorer: failingScorer(stage.KindUnavailable), FailOnError: true}}

	if err := harness.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []string
	for _, failure := range harness.engine.StageFailures {
		got = append(got, failure.File)
	}

	if strings.Join(got, ",") != "a.go,m.go,z.go" {
		t.Errorf("failure order = %v, want sorted by file", got)
	}
}

func TestPipeline_StageFailureJSONCarriesKind(t *testing.T) {
	b, err := json.Marshal(analysis.StageFailure{Stage: "rerank", File: "a.go", Kind: stage.KindPreconditionNotMet, Error: "boom"})
	if err != nil {
		t.Fatal(err)
	}

	want := `{"node":"rerank","file":"a.go","kind":"precondition_not_met","error":"boom"}`
	if string(b) != want {
		t.Errorf("json = %s, want %s", b, want)
	}
}

func TestPipeline_ScorerErrorSkipsFile(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "svc.go", "package svc")
	h.engine.Stages = []stage.Stage{{Scorer: scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		return nil, errors.New("scorer down")
	})}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.engine.SkippedFiles) != 1 || len(h.judged) != 0 {
		t.Fatalf("SkippedFiles = %d, judged = %v; want the file skipped with nothing judged", len(h.engine.SkippedFiles), h.judged)
	}
}

func TestPipeline_PassThroughStageErrorJudgesEveryCandidate(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "svc.go", "package svc")
	var logged bytes.Buffer
	h.engine.Out = output.New(&logged, false)
	h.engine.JSONOutput = true
	h.engine.Stages = []stage.Stage{{Name: "screen", PassThroughOnSkip: true, Scorer: scorerFunc(func(ctx context.Context, file stage.File, debug stage.Debug, candidates []stage.Candidate) ([]float64, error) {
		return nil, errors.New("scorer down")
	})}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.engine.SkippedFiles) != 0 || len(h.judged) != 2 {
		t.Errorf("SkippedFiles = %d, judged = %v; want both ADRs judged and the file not skipped", len(h.engine.SkippedFiles), h.judged)
	}

	if !strings.Contains(logged.String(), "node screen skipped, 2 candidate(s) pass through unchanged") || !strings.Contains(logged.String(), "scorer down") {
		t.Errorf("log %q lacks a warning naming the node and the error", logged.String())
	}
}

func TestPipeline_SuppressedADRDoesNotConsumeARankSlot(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 0.9), scorerADR("0003", 0.8)}, "svc.go", "// archguard-ignore: 0001\npackage svc")
	h.engine.Config.Analysis.MaxRelevantADRs = 2
	h.engine.Stages = defaultStages(h.engine.Config, h.engine.Store, h.engine.Embed)

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Join(h.judged, ",") != "0002,0003" {
		t.Fatalf("judged %v, want the two highest-ranked unsuppressed ADRs (0002,0003)", h.judged)
	}
}

func TestPipeline_RankEmbedsOncePerFile(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 0.9)}, "svc.go", "package svc")

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.embeds != 1 {
		t.Fatalf("made %d embedding calls, want 1 for one file", h.embeds)
	}

	if len(h.judged) != 2 {
		t.Fatalf("judged %v, want both ADRs", h.judged)
	}
}

func TestPipeline_NoStagesFailsInsteadOfJudgingEveryADR(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 0.9)}, "svc.go", "package svc")
	h.engine.Stages = nil

	if err := h.engine.Run(t.Context()); err == nil {
		t.Fatal("expected an error for an engine with no stages, got nil")
	}

	if len(h.judged) != 0 || h.embeds != 0 {
		t.Fatalf("judged %v with %d embeds, want nothing done", h.judged, h.embeds)
	}
}

type failingScopedStore struct{ index.VectorStore }

func (failingScopedStore) ScopedADRs(string) ([]index.SearchResult, error) {
	return nil, errors.New("db down")
}

func TestPipeline_CandidateLoadFailureSkipsTheFileInsteadOfPassingIt(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "svc.go", "package svc")
	h.engine.Store = failingScopedStore{VectorStore: h.engine.Store}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.engine.SkippedFiles) != 1 || len(h.judged) != 0 || h.embeds != 0 {
		t.Fatalf("SkippedFiles = %d, judged = %v, embeds = %d; want the file reported as skipped with nothing judged", len(h.engine.SkippedFiles), h.judged, h.embeds)
	}
}

type screenerFunc func(state string, questions map[string]string) (map[string]float64, error)

func (f screenerFunc) Screen(_ context.Context, state string, questions map[string]string) (map[string]float64, error) {
	return f(state, questions)
}

func ruledADR(id, statement string) index.ADR {
	adr := scorerADR(id, 1)
	adr.Rules = index.Rules{{Statement: statement}}
	return adr
}

type askRun struct {
	onError     string
	content     string
	judgeChange bool
}

func runAsk(t *testing.T, run askRun, screener stage.Screener) (*scorerHarness, string) {
	t.Helper()

	content := run.content
	if content == "" {
		content = "package svc"
	}

	h := newScorerHarness(t, []index.ADR{ruledADR("0001", "no prints"), ruledADR("0002", "no globals"), scorerADR("0003", 1)}, "svc.go", content)
	h.engine.JudgeChange = run.judgeChange
	var logged bytes.Buffer
	h.engine.Out = output.New(&logged, false)
	h.engine.JSONOutput = true

	onError := run.onError
	if onError == "" {
		onError = config.OnErrorSkip
	}

	nodes := []config.Node{
		{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, TopK: 3, OnError: config.OnErrorSkip},
		{Name: "breaks-a-rule", Job: config.JobAsk, With: config.RunnerJev, Threshold: 0.3, OnError: onError, Question: config.Question{Instructions: config.BreaksARuleInstructions}},
	}

	h.engine.Stages = analysis.BuildPipeline(nodes, h.engine.Store, h.engine.Embed, fixedScreeners{screener: screener})
	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return h, logged.String()
}

func TestPipeline_AskSendsOnlyScreenedInADRsToTheLLM(t *testing.T) {
	var states []string
	screener := screenerFunc(func(state string, questions map[string]string) (map[string]float64, error) {
		states = append(states, state)
		answers := make(map[string]float64, len(questions))
		for name, question := range questions {
			answers[name] = 0.05
			if strings.Contains(question, "no prints") {
				answers[name] = 0.95
			}
		}

		return answers, nil
	})

	h, _ := runAsk(t, askRun{}, screener)

	slices.Sort(h.judged)

	if strings.Join(h.judged, ",") != "0001,0003" {
		t.Errorf("judged %v, want 0001 (rule likely broken) and 0003 (no rules), not 0002", h.judged)
	}

	if len(states) != 1 || states[0] != "package svc" {
		t.Errorf("screener states = %q, want one call with the content the LLM judges", states)
	}

	if stats := h.engine.CollectedStages; len(stats) != 2 || stats[1].Name != "breaks-a-rule" || stats[1].Received != 3 || stats[1].Kept != 2 {
		t.Errorf("node stats = %+v, want breaks-a-rule receiving 3 and keeping 2", stats)
	}
}

func TestPipeline_AskSkipPassesCandidatesToTheLLM(t *testing.T) {
	down := screenerFunc(func(string, map[string]string) (map[string]float64, error) {
		return nil, errors.New("jev down")
	})

	h, logged := runAsk(t, askRun{}, down)

	if len(h.judged) != 3 || len(h.engine.SkippedFiles) != 0 {
		t.Errorf("judged %v with %d skipped file(s), want all 3 ADRs judged and the file not skipped", h.judged, len(h.engine.SkippedFiles))
	}

	if !strings.Contains(logged, "breaks-a-rule") || !strings.Contains(logged, "jev down") {
		t.Errorf("log %q lacks a warning naming the node and the error", logged)
	}
}

func TestPipeline_AskFailRecordsANodeFailure(t *testing.T) {
	down := screenerFunc(func(string, map[string]string) (map[string]float64, error) {
		return nil, errors.New("jev down")
	})

	h, _ := runAsk(t, askRun{onError: config.OnErrorFail}, down)

	if len(h.judged) != 0 || len(h.engine.StageFailures) != 1 || h.engine.StageFailures[0].Stage != "breaks-a-rule" {
		t.Errorf("judged %v, failures %+v; want nothing judged and one breaks-a-rule failure", h.judged, h.engine.StageFailures)
	}
}

// screenerBreaking answers 0.9 to every question about a state containing code, 0.05 otherwise, and records each state.
func screenerBreaking(code string, states *[]string) stage.Screener {
	return screenerFunc(func(state string, questions map[string]string) (map[string]float64, error) {
		*states = append(*states, state)
		answers := make(map[string]float64, len(questions))
		for name := range questions {
			answers[name] = 0.05
			if strings.Contains(state, code) {
				answers[name] = 0.9
			}
		}

		return answers, nil
	})
}

func TestPipeline_AskReadsEachSideOfADiffAsPlainText(t *testing.T) {
	tests := []struct {
		name       string
		diff       string
		breaking   string
		wantStates []string
		wantJudged string
	}{
		{
			name:       "removing the code a rule needs is caught on the before side",
			diff:       "@@ -1,2 +1,1 @@\n route()\n-withGate()\n",
			breaking:   "withGate()",
			wantStates: []string{"route()\nwithGate()", "route()"},
			wantJudged: "0001,0002,0003",
		},
		{
			name:       "a hunk that only removes lines is asked about once",
			diff:       "@@ -1,1 +0,0 @@\n-withGate()\n",
			breaking:   "nothing breaks",
			wantStates: []string{"withGate()"},
			wantJudged: "0003",
		},
		{
			name:       "a hunk that only adds lines is asked about once",
			diff:       "@@ -0,0 +1,1 @@\n+fmt.Println(x)\n",
			breaking:   "fmt.Println",
			wantStates: []string{"fmt.Println(x)"},
			wantJudged: "0001,0002,0003",
		},
		{
			name:       "a mixed hunk is asked about on both sides",
			diff:       "@@ -1 +1 @@\n-old()\n+fmt.Println(x)\n",
			breaking:   "fmt.Println",
			wantStates: []string{"old()", "fmt.Println(x)"},
			wantJudged: "0001,0002,0003",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var states []string

			h, _ := runAsk(t, askRun{content: tt.diff, judgeChange: true}, screenerBreaking(tt.breaking, &states))

			if !slices.Equal(states, tt.wantStates) {
				t.Errorf("screener states = %q, want %q", states, tt.wantStates)
			}

			slices.Sort(h.judged)

			if strings.Join(h.judged, ",") != tt.wantJudged {
				t.Errorf("judged %v, want %s", h.judged, tt.wantJudged)
			}
		})
	}
}

func TestPipeline_AskReadsBothSidesOfALargeFilesDiff(t *testing.T) {
	var states []string

	h := newScorerHarness(t, []index.ADR{ruledADR("0001", "no prints")}, "svc.go", strings.Repeat("x ", 50))
	h.engine.Config.LLM.MaxTokens = 1
	h.engine.Content = &diffCapableContentProvider{content: strings.Repeat("x ", 50), diff: "@@ -1 +1 @@\n-old()\n+fmt.Println(x)\n"}

	nodes := []config.Node{
		{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, TopK: 3, OnError: config.OnErrorSkip},
		{Name: "breaks-a-rule", Job: config.JobAsk, With: config.RunnerJev, Threshold: 0.3, OnError: config.OnErrorSkip},
	}
	screener := screenerBreaking("fmt.Println", &states)
	h.engine.Stages = analysis.BuildPipeline(nodes, h.engine.Store, h.engine.Embed, fixedScreeners{screener: screener})

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(states, []string{"old()", "fmt.Println(x)"}) {
		t.Errorf("screener states = %q, want the diff's two sides", states)
	}
}
