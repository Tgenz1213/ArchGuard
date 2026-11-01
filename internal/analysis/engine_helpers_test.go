package analysis_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"gopkg.in/yaml.v3"
)

type providers struct {
	chat  inference.Chatter
	embed inference.Embedder
}

// both uses one provider for chat and embeddings, as most tests do.
func both(p interface {
	inference.Chatter
	inference.Embedder
}) providers {
	return providers{chat: p, embed: p}
}

// newEngine builds an engine running cfg's pipeline, or the one archguard init writes when cfg has none, parsed and built as check does.
// Settings in cfg are read here, as check reads them once at startup, so set them before calling it.
func newEngine(t *testing.T, cfg *config.Config, store index.VectorStore, p providers, content analysis.ContentProvider) *analysis.Engine {
	t.Helper()

	if cfg.Analysis.Pipeline.Kind == 0 {
		if err := yaml.Unmarshal([]byte(config.DefaultPipeline), &cfg.Analysis); err != nil {
			t.Fatalf("default pipeline doesn't parse: %v", err)
		}
	}

	settings, err := config.ParsePipeline(cfg)
	if err != nil {
		t.Fatalf("pipeline is invalid for this config: %v", err)
	}

	engine := analysis.NewEngine(cfg, store, p.chat, p.embed, content)
	engine.Nodes = analysis.BuildPipeline(settings, store, p.embed, fixedScreeners{screener: unexpectedScreener{t}})

	return engine
}

// unexpectedScreener fails a test whose pipeline asks Jev without giving the engine a screener.
type unexpectedScreener struct{ t *testing.T }

func (u unexpectedScreener) Screen(context.Context, string, map[string]string) (map[string]float64, error) {
	u.t.Error("an ask node ran with no screener; build the nodes with one")

	return nil, errors.New("no screener in this test")
}

// fixedScreeners gives every ask node the same screener and records the models asked for.
type fixedScreeners struct {
	screener node.Screener
	models   *[]string
}

func (f fixedScreeners) ForModel(model string) node.Screener {
	if f.models != nil {
		*f.models = append(*f.models, model)
	}

	return f.screener
}
