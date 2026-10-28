package analysis

import (
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

const (
	rerankDefaultThreshold = 0.0
	rerankDefaultTopK      = 3
)

// StageDeps are what the configured scorers call; each is needed only by the scorers that use it.
type StageDeps struct {
	Store     index.VectorStore
	Embed     inference.Embedder
	Screener  stage.Screener
	Questions stage.QuestionSource
}

// BuildStages returns nil when no pipeline is configured, leaving Engine on its default stages.
func BuildStages(cfg *config.Config, deps StageDeps, out *output.Printer) []stage.Stage {
	pipeline := cfg.Analysis.Pipeline
	if pipeline == nil {
		return nil
	}

	stages := []stage.Stage{rankStage(cfg, pipeline.Rank, deps.Store, deps.Embed)}
	if pipeline.Rerank != nil {
		stages = append(stages, rerankStage(pipeline.Rerank, deps.Store, deps.Embed, out))
	}

	if pipeline.PreJudge != nil {
		stages = append(stages, preJudgeStage(pipeline.PreJudge, deps))
	}

	return stages
}

// config guarantees a pre_judge stage is jev with a threshold.
func preJudgeStage(sc *config.StageConfig, deps StageDeps) stage.Stage {
	topK := 0
	if sc.TopK != nil {
		topK = *sc.TopK
	}

	var generated stage.QuestionSource
	if sc.Jev.GenerateQuestions {
		generated = deps.Questions
	}

	st := stage.NewJevStage(deps.Screener, generated, *sc.Threshold, topK)
	st.FailOnError = sc.OnError == config.OnErrorFail
	return st
}

func rankStage(cfg *config.Config, sc *config.StageConfig, store index.VectorStore, embed inference.Embedder) stage.Stage {
	threshold := cfg.VectorStore.SimilarityThreshold
	topK := cfg.Analysis.RelevantADRLimit()

	if sc != nil {
		if sc.Threshold != nil {
			threshold = *sc.Threshold
		}

		if sc.TopK != nil {
			topK = *sc.TopK
		}
	}

	st := stage.NewCosineStage(store, embed, threshold, topK)
	if sc != nil {
		st.FailOnError = sc.OnError == config.OnErrorFail
	}

	return st
}

func rerankStage(sc *config.StageConfig, store index.VectorStore, embed inference.Embedder, out *output.Printer) stage.Stage {
	threshold := rerankDefaultThreshold
	if sc.Threshold != nil {
		threshold = *sc.Threshold
	} else {
		out.Warn("analysis.pipeline.rerank.threshold not set, defaulting to %v", rerankDefaultThreshold)
	}

	topK := rerankDefaultTopK
	if sc.TopK != nil {
		topK = *sc.TopK
	} else {
		out.Warn("analysis.pipeline.rerank.top_k not set, defaulting to %d", rerankDefaultTopK)
	}

	st := stage.NewCosineStage(store, embed, threshold, topK)
	st.Name = "rerank"
	st.FailOnError = sc.OnError == config.OnErrorFail
	return st
}
