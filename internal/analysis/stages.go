package analysis

import (
	"fmt"
	"io"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

const (
	rerankDefaultThreshold = 0.0
	rerankDefaultTopK      = 3
)

// BuildStages returns nil when no pipeline is configured, leaving Engine on its default stages.
func BuildStages(cfg *config.Config, store index.VectorStore, embed llm.Embedder, warnings io.Writer) []stage.Stage {
	pipeline := cfg.Analysis.Pipeline
	if pipeline == nil {
		return nil
	}

	stages := []stage.Stage{rankStage(cfg, pipeline.Rank, store, embed)}
	if pipeline.Rerank != nil {
		stages = append(stages, rerankStage(pipeline.Rerank, store, embed, warnings))
	}
	return stages
}

func rankStage(cfg *config.Config, sc *config.StageConfig, store index.VectorStore, embed llm.Embedder) stage.Stage {
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

func rerankStage(sc *config.StageConfig, store index.VectorStore, embed llm.Embedder, warnings io.Writer) stage.Stage {
	threshold := rerankDefaultThreshold
	if sc.Threshold != nil {
		threshold = *sc.Threshold
	} else {
		_, _ = fmt.Fprintf(warnings, "Warning: analysis.pipeline.rerank.threshold not set, defaulting to %v\n", rerankDefaultThreshold)
	}

	topK := rerankDefaultTopK
	if sc.TopK != nil {
		topK = *sc.TopK
	} else {
		_, _ = fmt.Fprintf(warnings, "Warning: analysis.pipeline.rerank.top_k not set, defaulting to %d\n", rerankDefaultTopK)
	}
	st := stage.NewCosineStage(store, embed, threshold, topK)
	st.Name = "rerank"
	st.FailOnError = sc.OnError == config.OnErrorFail
	return st
}
