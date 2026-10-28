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
	Jev       func(*config.JevOptions) stage.Screener
	Questions stage.QuestionSource
}

// BuildStages returns nil when no pipeline is configured, leaving Engine on its default stages.
func BuildStages(cfg *config.Config, deps StageDeps, out *output.Printer) []stage.Stage {
	pipeline := cfg.Analysis.Pipeline
	if pipeline == nil {
		return nil
	}

	b := stageBuilder{cfg: cfg, deps: deps, out: out}

	stages := []stage.Stage{b.rank(pipeline.Rank)}
	if pipeline.Rerank != nil {
		stages = append(stages, b.rerank(pipeline.Rerank))
	}

	if pipeline.PreJudge != nil {
		stages = append(stages, b.preJudge(pipeline.PreJudge))
	}

	return stages
}

type stageBuilder struct {
	cfg  *config.Config
	deps StageDeps
	out  *output.Printer
}

// config guarantees a pre_judge stage is jev with a threshold.
func (b stageBuilder) preJudge(sc *config.StageConfig) stage.Stage {
	topK := 0
	if sc.TopK != nil {
		topK = *sc.TopK
	}

	var generated stage.QuestionSource
	if sc.Jev.GenerateQuestions {
		generated = b.deps.Questions
	}

	st := stage.NewJevStage(b.deps.Jev(sc.Jev), generated, *sc.Threshold, topK)
	st.FailOnError = sc.OnError == config.OnErrorFail
	return st
}

func (b stageBuilder) rank(sc *config.StageConfig) stage.Stage {
	threshold := b.cfg.VectorStore.SimilarityThreshold
	topK := b.cfg.Analysis.RelevantADRLimit()

	if sc == nil {
		return stage.NewCosineStage(b.deps.Store, b.deps.Embed, threshold, topK)
	}

	if sc.Threshold != nil {
		threshold = *sc.Threshold
	}

	if sc.TopK != nil {
		topK = *sc.TopK
	}

	return b.ranker("rank", sc, threshold, topK)
}

func (b stageBuilder) rerank(sc *config.StageConfig) stage.Stage {
	threshold := rerankDefaultThreshold
	if sc.Threshold != nil {
		threshold = *sc.Threshold
	} else {
		b.out.Warn("analysis.pipeline.rerank.threshold not set, defaulting to %v", rerankDefaultThreshold)
	}

	topK := rerankDefaultTopK
	if sc.TopK != nil {
		topK = *sc.TopK
	} else {
		b.out.Warn("analysis.pipeline.rerank.top_k not set, defaulting to %d", rerankDefaultTopK)
	}

	return b.ranker("rerank", sc, threshold, topK)
}

func (b stageBuilder) ranker(name string, sc *config.StageConfig, threshold float64, topK int) stage.Stage {
	if sc.Scorer != config.ScorerJev {
		st := stage.NewCosineStage(b.deps.Store, b.deps.Embed, threshold, topK)
		st.Name = name
		st.FailOnError = sc.OnError == config.OnErrorFail
		return st
	}

	st := stage.NewJevRankStage(name, b.deps.Jev(sc.Jev), threshold, topK)

	// Skipping the only ranker would send every scope-matched ADR to the LLM, so it fails unless on_error says skip.
	st.FailOnError = sc.OnError == config.OnErrorFail || (sc.OnError == "" && b.cfg.Analysis.Pipeline.JevIsOnlyRanker())
	return st
}
