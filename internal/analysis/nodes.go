package analysis

import (
	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

// Screeners gives each ask node the screener for the Jev model it names.
type Screeners interface {
	ForModel(model string) node.Screener
}

// BuildPipeline returns the nodes that choose candidates; the judge node is the engine's own judging step.
func BuildPipeline(settings []config.Node, store index.VectorStore, embed inference.Embedder, screeners Screeners) []node.Node {
	var nodes []node.Node

	for _, s := range settings {
		switch s.Job {
		case config.JobRank:
			nodes = append(nodes, rankNode(s, store, embed))
		case config.JobAsk:
			nodes = append(nodes, askNode(s, screeners.ForModel(s.Model)))
		}
	}

	return nodes
}

func rankNode(s config.Node, store index.VectorStore, embed inference.Embedder) node.Node {
	n := node.NewCosineNode(store, embed, s.Threshold, s.TopK)
	n.Name = s.Name
	n.FailOnError = s.OnError == config.OnErrorFail

	return n
}

func askNode(s config.Node, screener node.Screener) node.Node {
	n := node.NewJevNode(screener, s.Question.Instructions, s.Threshold, s.TopK)
	n.Name = s.Name
	n.FailOnError = s.OnError == config.OnErrorFail

	return n
}
