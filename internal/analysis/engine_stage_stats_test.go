package analysis_test

import (
	"context"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
)

func TestEngine_CollectsStageStatsInPipelineOrderUnderJSONOutput(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.JSONOutput = true
	scores := map[string]float64{"0001": 0.9, "0002": 0.8}
	h.engine.Stages = []stage.Stage{
		{Name: "rank", Scorer: scoresByID(scores, nil)},
		{Name: "rerank", Scorer: scoresByID(scores, nil), MaxKeep: 1},
	}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := h.engine.CollectedStages
	if len(got) != 2 || got[0].Name != "rank" || got[0].Received != 2 || got[0].Kept != 2 || got[1].Name != "rerank" || got[1].Received != 2 || got[1].Kept != 1 {
		t.Fatalf("stages = %+v, want rank 2/2 then rerank 2/1", got)
	}
}

func TestEngine_ListsEveryStageEvenWhenThereAreNoFiles(t *testing.T) {
	h := newScorerHarness(t, nil, "a.go", "package a")
	h.engine.Content.(*MockContentProvider).Files = map[string]string{}
	h.engine.JSONOutput = true
	h.engine.Stages = []stage.Stage{{Name: "rank", Scorer: scoresByID(nil, nil)}, {Name: "rerank", Scorer: scoresByID(nil, nil)}}

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := h.engine.CollectedStages; len(got) != 2 || got[0].Name != "rank" || got[1].Name != "rerank" {
		t.Fatalf("stages = %+v, want rank and rerank listed", got)
	}
}

func TestEngine_DefaultStageIsReportedAsRank(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.JSONOutput = true

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := h.engine.CollectedStages
	if len(got) != 1 || got[0].Name != "rank" || got[0].Received != 2 || got[0].Kept != 2 {
		t.Fatalf("stages = %+v, want the default stage as rank with 2 received and 2 kept", got)
	}
}

func TestEngine_CollectsNoStageStatsOutsideJSONOutput(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")

	if err := h.engine.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.engine.CollectedStages != nil {
		t.Fatalf("stages = %+v, want none collected outside JSON mode", h.engine.CollectedStages)
	}
}
