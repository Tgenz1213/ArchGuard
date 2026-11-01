package analysis_test

import (
	"fmt"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"gopkg.in/yaml.v3"
)

// newEngine builds an engine that runs the pipeline archguard init writes, parsed and built as check does.
func newEngine(cfg *config.Config, store index.VectorStore, chat inference.Chatter, embed inference.Embedder, content analysis.ContentProvider) *analysis.Engine {
	engine := analysis.NewEngine(cfg, store, chat, embed, content)
	engine.Stages = defaultStages(cfg, store, embed)

	return engine
}

// defaultStages reads max_relevant_adrs and similarity_threshold from cfg when called, as check does once at startup.
// It panics on a config the default pipeline can't run on, which fails the calling test.
func defaultStages(cfg *config.Config, store index.VectorStore, embed inference.Embedder) []stage.Stage {
	var analysisBlock struct {
		Pipeline yaml.Node `yaml:"pipeline"`
	}
	if err := yaml.Unmarshal([]byte(config.DefaultPipeline), &analysisBlock); err != nil {
		panic(fmt.Sprintf("default pipeline doesn't parse: %v", err))
	}

	cfg.Analysis.Pipeline = analysisBlock.Pipeline

	nodes, err := config.ParsePipeline(cfg)
	if err != nil {
		panic(fmt.Sprintf("default pipeline is invalid for this config: %v", err))
	}

	return analysis.BuildPipeline(nodes, store, embed, fixedScreeners{})
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
