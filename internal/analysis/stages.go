package analysis

import (
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

// NewScreener returns the screener an ask node calls, for the Jev model it names.
type NewScreener func(model string) stage.Screener

// BuildPipeline returns the stages that choose candidates; the judge node is the engine's own judging step.
func BuildPipeline(nodes []config.Node, store index.VectorStore, embed inference.Embedder, newScreener NewScreener) []stage.Stage {
	var stages []stage.Stage

	for _, node := range nodes {
		switch node.Job {
		case config.JobRank:
			stages = append(stages, rankStage(node, store, embed))
		case config.JobAsk:
			stages = append(stages, askStage(node, newScreener(node.Model)))
		}
	}

	return stages
}

func rankStage(node config.Node, store index.VectorStore, embed inference.Embedder) stage.Stage {
	st := stage.NewCosineStage(store, embed, node.Threshold, node.TopK)
	st.Name = node.Name
	st.FailOnError = node.OnError == config.OnErrorFail

	return st
}

func askStage(node config.Node, screener stage.Screener) stage.Stage {
	st := stage.NewJevStage(screener, node.Question.Instructions, node.Threshold, node.TopK)
	st.Name = node.Name
	st.FailOnError = node.OnError == config.OnErrorFail

	return st
}
