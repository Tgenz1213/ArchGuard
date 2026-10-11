package cli

import (
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

// The run always records into a Tally for its report, and into the caller's recorder too when one was injected.
func newRecorder(tally *telemetry.Tally, injected telemetry.Recorder) telemetry.Recorder {
	if injected == nil {
		return tally
	}

	return telemetry.Fanout{tally, injected}
}

func usageReport(cfg *config.Config, suggestFixes bool, tally *telemetry.Tally) *output.Usage {
	judgment := tally.Totals(telemetry.Judgment)

	usage := &output.Usage{
		Judgment:  withCounts(chatUsage(judgment.Usage, cfg.LLM), judgment),
		Embedding: embeddingReport(cfg, tally),
		Files:     fileDurations(tally),
	}

	if suggestFixes {
		totals := tally.Totals(telemetry.Suggestion)
		suggestion := withCounts(chatUsage(totals.Usage, cfg.LLM), totals)
		usage.Suggestion = &suggestion
	}

	return usage
}

// A dry run sends no judgment, so only its input is known and priced.
func dryRunUsage(cfg *config.Config, tally *telemetry.Tally, projected inference.Usage) *output.Usage {
	judgment := projectedChatUsage(projected, cfg.LLM.CostPer1KInputTokens)
	judgment.ChatCounts = cacheCounts(tally.Totals(telemetry.Judgment))

	return &output.Usage{
		Judgment:  judgment,
		Embedding: embeddingReport(cfg, tally),
		Files:     fileDurations(tally),
	}
}

func withCounts(usage output.ChatUsage, totals telemetry.Totals) output.ChatUsage {
	usage.Calls = totals.Calls
	usage.ChatCounts = cacheCounts(totals)

	return usage
}

func cacheCounts(totals telemetry.Totals) output.ChatCounts {
	return output.ChatCounts{CacheHits: totals.CacheHits, CacheMisses: totals.CacheMisses, Retries: totals.Retries}
}

func embeddingReport(cfg *config.Config, tally *telemetry.Tally) output.EmbeddingUsage {
	totals := tally.Totals(telemetry.Embedding)
	usage := embeddingUsage(totals.Usage, cfg.VectorStore.CostPer1KTokens)
	usage.Calls = totals.Calls

	return usage
}

func fileDurations(tally *telemetry.Tally) []output.FileDuration {
	durations := []output.FileDuration{}
	for _, file := range tally.Files() {
		durations = append(durations, output.FileDuration{File: file.File, DurationMS: file.Spent.Milliseconds()})
	}

	return durations
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
