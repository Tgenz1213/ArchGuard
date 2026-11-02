package analysis_test

import (
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

func ptr[T any](v T) *T { return &v }

func buildPipeline(t *testing.T, settings ...config.Node) []node.Node {
	t.Helper()

	var models []string

	nodes := analysis.BuildPipeline(settings, index.NewLocalStore(1), &inference.MockProvider{}, fixedAskers{models: &models})
	if len(models) != countJob(settings, config.JobAsk) {
		t.Errorf("built %d askers, want one per ask node", len(models))
	}

	return nodes
}

func countJob(settings []config.Node, job string) int {
	count := 0
	for _, s := range settings {
		if s.Job == job {
			count++
		}
	}

	return count
}

func cosineSettings(t *testing.T, rank node.Node) (threshold float64, topK int) {
	t.Helper()

	ranker, ok := rank.Scorer.(*node.CosineRanker)
	if !ok {
		t.Fatalf("scorer = %T, want *node.CosineRanker", rank.Scorer)
	}

	minimum, ok := rank.Min.(node.ADRThreshold)
	if !ok || minimum.Global != ranker.Threshold {
		t.Fatalf("Min = %#v, want ADRThreshold matching the ranker threshold %v", rank.Min, ranker.Threshold)
	}

	return ranker.Threshold, rank.MaxKeep
}

var judgeNode = config.Node{Name: "judge", Job: config.JobJudge, With: config.RunnerLLM}

func TestBuildPipeline_OneNodePerChoosingNodeInOrder(t *testing.T) {
	nodes := buildPipeline(t,
		config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, Threshold: 0.3, TopK: 8, OnError: config.OnErrorSkip},
		config.Node{Name: "rerank", Job: config.JobRank, With: config.RunnerCosine, Threshold: 0.6, TopK: 2, OnError: config.OnErrorFail},
		judgeNode,
	)
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}

	if nodes[0].Name != "shortlist" || nodes[1].Name != "rerank" {
		t.Errorf("names = %q, %q, want shortlist, rerank", nodes[0].Name, nodes[1].Name)
	}

	if threshold, topK := cosineSettings(t, nodes[0]); threshold != 0.3 || topK != 8 {
		t.Errorf("shortlist = (%v, %d), want (0.3, 8)", threshold, topK)
	}

	if threshold, topK := cosineSettings(t, nodes[1]); threshold != 0.6 || topK != 2 {
		t.Errorf("rerank = (%v, %d), want (0.6, 2)", threshold, topK)
	}

	if nodes[0].FailOnError || !nodes[1].FailOnError {
		t.Errorf("FailOnError = %v, %v, want false, true", nodes[0].FailOnError, nodes[1].FailOnError)
	}
}

func TestBuildPipeline_AskNodeScreensWithItsInstructions(t *testing.T) {
	ask := config.Node{
		Name: "screen", Job: config.JobAsk, With: config.RunnerJev, Threshold: 0.4, TopK: 2, OnError: config.OnErrorFail, Model: "jev-2",
		Question: config.Question{Type: config.QuestionNoul, ForEach: config.ForEachRule, Instructions: "Broken? {rule}"},
	}
	nodes := buildPipeline(t, config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, TopK: 5}, ask, judgeNode)

	built := nodes[1]

	scorer, ok := built.Scorer.(*node.RuleAsk)
	if !ok {
		t.Fatalf("scorer = %T, want *node.RuleAsk", built.Scorer)
	}

	if built.Name != "screen" || scorer.Instructions != "Broken? {rule}" || built.MaxKeep != 2 || !built.FailOnError || !built.PassThroughOnSkip {
		t.Errorf("ask node = %+v, scorer %+v", built, scorer)
	}

	if got := built.Min.For(&index.ADR{}); got != 0.4 {
		t.Errorf("threshold = %v, want 0.4", got)
	}
}

func TestBuildPipeline_ADROverrideBeatsRankThreshold(t *testing.T) {
	nodes := buildPipeline(t, config.Node{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine, Threshold: 0.8, TopK: 5}, judgeNode)

	own := &index.ADR{SimilarityThreshold: ptr(0.1)}
	if got := nodes[0].Min.For(own); got != 0.1 {
		t.Errorf("threshold for ADR with override = %v, want 0.1", got)
	}

	if got := nodes[0].Min.For(&index.ADR{}); got != 0.8 {
		t.Errorf("threshold for ADR without override = %v, want 0.8", got)
	}
}
