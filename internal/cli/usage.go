package cli

import (
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

// Judgment and suggestion calls share one chat provider through separate meters, so each is totalled on its own.
type usageMeters struct {
	judgment   *inference.MeteredChatter
	suggestion *inference.MeteredChatter
	embedding  *inference.MeteredEmbedder
}

func newUsageMeters(setup runSetup) usageMeters {
	return usageMeters{
		judgment:   inference.NewMeteredChatter(setup.chat),
		suggestion: inference.NewMeteredChatter(setup.chat),
		embedding:  inference.NewMeteredEmbedder(setup.embed),
	}
}

func (m usageMeters) report(cfg *config.Config, suggestFixes bool, engine *analysis.Engine) *output.Usage {
	judgment := chatUsage(m.judgment.Usage(), cfg.LLM)
	judgment.Calls = m.judgment.Calls()
	judgment.ChatCounts = engine.JudgmentCounts

	usage := &output.Usage{
		Judgment:  judgment,
		Embedding: m.embeddingUsage(cfg),
		Files:     nonNil(engine.FileDurations),
	}

	if suggestFixes {
		suggestion := chatUsage(m.suggestion.Usage(), cfg.LLM)
		suggestion.Calls = m.suggestion.Calls()
		suggestion.ChatCounts = engine.SuggestionCounts
		usage.Suggestion = &suggestion
	}

	return usage
}

// A dry run sends no judgment, so only its input is known and priced.
func (m usageMeters) dryRunReport(cfg *config.Config, engine *analysis.Engine) *output.Usage {
	judgment := projectedChatUsage(engine.Projection.Usage, cfg.LLM.CostPer1KInputTokens)
	judgment.ChatCounts = engine.JudgmentCounts

	return &output.Usage{
		Judgment:  judgment,
		Embedding: m.embeddingUsage(cfg),
		Files:     nonNil(engine.FileDurations),
	}
}

func (m usageMeters) embeddingUsage(cfg *config.Config) output.EmbeddingUsage {
	usage := embeddingUsage(m.embedding.Usage(), cfg.VectorStore.CostPer1KTokens)
	usage.Calls = m.embedding.Calls()

	return usage
}

func projectedChatUsage(usage inference.Usage, costPer1KInput *float64) output.ChatUsage {
	if !usage.Reported {
		return output.ChatUsage{}
	}

	report := output.ChatUsage{InputTokens: &usage.InputTokens}
	if costPer1KInput != nil {
		cost := price(usage.InputTokens, *costPer1KInput)
		report.Cost = &cost
	}

	return report
}

func chatUsage(usage inference.Usage, llm config.LLMConfig) output.ChatUsage {
	if !usage.Reported {
		return output.ChatUsage{}
	}

	report := output.ChatUsage{InputTokens: &usage.InputTokens, OutputTokens: &usage.OutputTokens}
	if llm.CostPer1KInputTokens != nil && llm.CostPer1KOutputTokens != nil {
		cost := price(usage.InputTokens, *llm.CostPer1KInputTokens) + price(usage.OutputTokens, *llm.CostPer1KOutputTokens)
		report.Cost = &cost
	}

	return report
}

func embeddingUsage(usage inference.Usage, costPer1K *float64) output.EmbeddingUsage {
	if !usage.Reported {
		return output.EmbeddingUsage{}
	}

	report := output.EmbeddingUsage{Tokens: &usage.InputTokens}
	if costPer1K != nil {
		cost := price(usage.InputTokens, *costPer1K)
		report.Cost = &cost
	}

	return report
}

func price(tokens int, costPer1K float64) float64 {
	return float64(tokens) / 1000 * costPer1K
}
