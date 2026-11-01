package analysis_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/index"
)

func TestEngine_CollectsNodeStatsInPipelineOrderUnderJSONOutput(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.JSONOutput = true
	scores := map[string]float64{"0001": 0.9, "0002": 0.8}
	h.engine.Nodes = []node.Node{
		{Name: "rank", Scorer: scoresByID(scores, nil)},
		{Name: "rerank", Scorer: scoresByID(scores, nil), MaxKeep: 1},
	}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := h.engine.CollectedNodes
	if len(got) != 2 || got[0].Name != "rank" || got[0].Received != 2 || got[0].Kept != 2 || got[1].Name != "rerank" || got[1].Received != 2 || got[1].Kept != 1 {
		t.Fatalf("nodes = %+v, want rank 2/2 then rerank 2/1", got)
	}
}

func TestEngine_ListsEveryNodeEvenWhenThereAreNoFiles(t *testing.T) {
	h := newScorerHarness(t, nil, "a.go", "package a")
	h.engine.Content.(*MockContentProvider).Files = map[string]string{}
	h.engine.JSONOutput = true
	h.engine.Nodes = []node.Node{{Name: "rank", Scorer: scoresByID(nil, nil)}, {Name: "rerank", Scorer: scoresByID(nil, nil)}}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := h.engine.CollectedNodes; len(got) != 2 || got[0].Name != "rank" || got[1].Name != "rerank" {
		t.Fatalf("nodes = %+v, want rank and rerank listed", got)
	}
}

type failingFilesProvider struct{ MockContentProvider }

func (failingFilesProvider) GetFiles(context.Context) ([]string, error) {
	return nil, errors.New("git failed")
}

func TestEngine_ListsEveryNodeWhenFileDiscoveryFails(t *testing.T) {
	h := newScorerHarness(t, nil, "a.go", "package a")
	h.engine.Content = &failingFilesProvider{}
	h.engine.JSONOutput = true
	h.engine.Nodes = []node.Node{{Name: "rank", Scorer: scoresByID(nil, nil)}, {Name: "rerank", Scorer: scoresByID(nil, nil)}}

	if err := h.engine.Run(t.Context()); err == nil {
		t.Fatal("expected the file discovery error")
	}

	if got := h.engine.CollectedNodes; len(got) != 2 || got[0].Name != "rank" || got[1].Name != "rerank" {
		t.Fatalf("nodes = %+v, want rank and rerank listed with zeros", got)
	}
}

func TestEngine_DefaultPipelineReportsItsShortlistNode(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.JSONOutput = true

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := h.engine.CollectedNodes
	if len(got) != 1 || got[0].Name != "shortlist" || got[0].Received != 2 || got[0].Kept != 2 {
		t.Fatalf("nodes = %+v, want the default pipeline's shortlist node with 2 received and 2 kept", got)
	}
}

func TestEngine_FailedNodeStillAppearsInNodeStatsAlongsideItsFailure(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1), scorerADR("0002", 1)}, "a.go", "package a")
	h.engine.JSONOutput = true
	h.engine.Nodes = []node.Node{
		{Name: "rank", Scorer: scoresByID(map[string]float64{"0001": 0.9, "0002": 0.8}, nil)},
		{Name: "rerank", FailOnError: true, Scorer: scorerFunc(func(context.Context, node.File, node.Debug, []node.Candidate) ([]float64, error) {
			return nil, errors.New("down")
		})},
	}

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(h.engine.NodeFailures) != 1 || h.engine.NodeFailures[0].Node != "rerank" {
		t.Fatalf("failures = %+v, want one failure from rerank", h.engine.NodeFailures)
	}

	got := h.engine.CollectedNodes
	if len(got) != 2 || got[0].Kept != 2 || got[1].Name != "rerank" || got[1].Received != 2 || got[1].Kept != 0 {
		t.Fatalf("nodes = %+v, want rank kept 2 and rerank received 2 kept 0", got)
	}
}

func TestEngine_CollectsNoNodeStatsOutsideJSONOutput(t *testing.T) {
	h := newScorerHarness(t, []index.ADR{scorerADR("0001", 1)}, "a.go", "package a")

	if err := h.engine.Run(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.engine.CollectedNodes != nil {
		t.Fatalf("nodes = %+v, want none collected outside JSON mode", h.engine.CollectedNodes)
	}
}
