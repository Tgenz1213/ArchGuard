package analysis_test

import (
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

// newEngine builds an engine whose only choosing stage is the default pipeline's cosine shortlist.
func newEngine(cfg *config.Config, store index.VectorStore, chat inference.Chatter, embed inference.Embedder, content analysis.ContentProvider) *analysis.Engine {
	engine := analysis.NewEngine(cfg, store, chat, embed, content)
	engine.Stages = []stage.Stage{stage.NewCosineStage(store, embed, cfg.VectorStore.SimilarityThreshold, cfg.Analysis.RelevantADRLimit())}

	return engine
}

// fixedScreeners gives every ask node the same screener and records the models asked for.
type fixedScreeners struct {
	screener stage.Screener
	models   *[]string
}

func (f fixedScreeners) ForModel(model string) stage.Screener {
	if f.models != nil {
		*f.models = append(*f.models, model)
	}

	return f.screener
}
