package analysis

import (
	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

// Askers gives each ask node the asker for the Jev model it names.
type Askers interface {
	ForModel(model string) node.Asker
}

// BuildPipeline returns the nodes that choose candidates; the judge node is the engine's own judging step.
func BuildPipeline(settings []config.Node, store index.VectorStore, embed inference.Embedder, askers Askers) []node.Node {
	var nodes []node.Node

	for _, s := range settings {
		switch s.Job {
		case config.JobRank:
			nodes = append(nodes, rankNode(s, store, embed))
		case config.JobAsk:
			nodes = append(nodes, askNode(s, askers.ForModel(s.Model)))
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

func askNode(s config.Node, asker node.Asker) node.Node {
	n := node.NewAskNode(asker, s.Question.Instructions, s.Threshold, s.TopK)
	n.Name = s.Name
	n.FailOnError = s.OnError == config.OnErrorFail

	return n
}
