package analysis_test

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

func ptr[T any](v T) *T { return &v }

func buildPipeline(t *testing.T, nodes ...config.Node) []stage.Stage {
	t.Helper()

	var models []string

	stages := analysis.BuildPipeline(nodes, index.NewLocalStore(1), &inference.MockProvider{}, fixedScreeners{models: &models})
	if len(models) != countJob(nodes, config.JobAsk) {
		t.Errorf("built %d screeners, want one per ask node", len(models))
	}

	return stages
}

func countJob(nodes []config.Node, job string) int {
	count := 0
	for _, node := range nodes {
		if node.Job == job {
			count++
		}
	}

	return count
}

func cosineSettings(t *testing.T, st stage.Stage) (threshold float64, topK int) {
	t.Helper()

	ranker, ok := st.Scorer.(*stage.CosineRanker)
	if !ok {
		t.Fatalf("scorer = %T, want *stage.CosineRanker", st.Scorer)
	}

	minimum, ok := st.Min.(stage.ADRThreshold)
	if !ok || minimum.Global != ranker.Threshold {
		t.Fatalf("Min = %#v, want ADRThreshold matching the ranker threshold %v", st.Min, ranker.Threshold)
	}

	return ranker.Threshold, st.MaxKeep
}

var judgeNode = config.Node{Name: "judge", Job: config.JobJudge, With: config.RunnerLLM}

func TestBuildPipeline_OneStagePerChoosingNodeInOrder(t *testing.T) {
	stages := buildPipeline(t,
		config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, Threshold: 0.3, TopK: 8, OnError: config.OnErrorSkip},
		config.Node{Name: "rerank", Job: config.JobRank, With: config.RunnerCosine, Threshold: 0.6, TopK: 2, OnError: config.OnErrorFail},
		judgeNode,
	)
	if len(stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(stages))
	}

	if stages[0].Name != "shortlist" || stages[1].Name != "rerank" {
		t.Errorf("names = %q, %q, want shortlist, rerank", stages[0].Name, stages[1].Name)
	}

	if threshold, topK := cosineSettings(t, stages[0]); threshold != 0.3 || topK != 8 {
		t.Errorf("shortlist = (%v, %d), want (0.3, 8)", threshold, topK)
	}

	if threshold, topK := cosineSettings(t, stages[1]); threshold != 0.6 || topK != 2 {
		t.Errorf("rerank = (%v, %d), want (0.6, 2)", threshold, topK)
	}

	if stages[0].FailOnError || !stages[1].FailOnError {
		t.Errorf("FailOnError = %v, %v, want false, true", stages[0].FailOnError, stages[1].FailOnError)
	}
}

func TestBuildPipeline_AskNodeScreensWithItsInstructions(t *testing.T) {
	ask := config.Node{
		Name: "screen", Job: config.JobAsk, With: config.RunnerJev, Threshold: 0.4, TopK: 2, OnError: config.OnErrorFail, Model: "jev-2",
		Question: config.Question{Type: config.QuestionNoul, ForEach: config.ForEachRule, Instructions: "Broken? {rule}"},
	}
	stages := buildPipeline(t, config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, TopK: 5}, ask, judgeNode)

	st := stages[1]

	screen, ok := st.Scorer.(*stage.JevScreen)
	if !ok {
		t.Fatalf("scorer = %T, want *stage.JevScreen", st.Scorer)
	}

	if st.Name != "screen" || screen.Instructions != "Broken? {rule}" || st.MaxKeep != 2 || !st.FailOnError || !st.PassThroughOnSkip {
		t.Errorf("ask stage = %+v, scorer %+v", st, screen)
	}

	if got := st.Min.For(&index.ADR{}); got != 0.4 {
		t.Errorf("threshold = %v, want 0.4", got)
	}
}

func TestBuildPipeline_ADROverrideBeatsRankThreshold(t *testing.T) {
	stages := buildPipeline(t, config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, Threshold: 0.8, TopK: 5}, judgeNode)

	own := &index.ADR{SimilarityThreshold: ptr(0.1)}
	if got := stages[0].Min.For(own); got != 0.1 {
		t.Errorf("threshold for ADR with override = %v, want 0.1", got)
	}

	if got := stages[0].Min.For(&index.ADR{}); got != 0.8 {
		t.Errorf("threshold for ADR without override = %v, want 0.8", got)
	}
}
