package analysis

import (
	"context"
	"fmt"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

// Projection is what a dry run's judgment calls would send; Usage is unreported when a count failed.
type Projection struct {
	Calls  int
	Cached int
	Usage  inference.Usage
}

// project counts the prompt instead of sending it; a cached verdict would cost nothing.
func (fc *fileCheck) project(ctx context.Context, hit stage.Candidate, input inference.DriftInput) {
	e := fc.engine

	_, found := fc.cachedVerdict(fc.analysisKey(hit))
	e.record(telemetry.CacheChecked{Role: telemetry.Judgment, Hit: found})

	if found {
		fc.out.Debug("%s", checkOutcomeLine(hit, "dry run", true))
		fc.findings.projection.Cached++

		return
	}

	tokens, err := e.Chat.CountTokens(ctx, e.systemPrompt()+"\n\n"+inference.DriftPrompt(input))
	if err != nil {
		fc.out.Warn("counting tokens failed: %v", err)
		fc.findings.failedChecks = append(fc.findings.failedChecks, output.FailedCheck{
			File:   fc.file,
			ADRID:  hit.ADR.ID,
			Title:  hit.ADR.Title,
			Reason: fmt.Sprintf("counting tokens: %v", err),
		})
		fc.findings.uncounted = true

		return
	}

	fc.out.Debug("%s", checkOutcomeLine(hit, fmt.Sprintf("dry run, %d input tokens", tokens), false))
	fc.findings.projection.Calls++
	fc.findings.projection.Usage.InputTokens += tokens
}

func (e *Engine) DryRunSummary() *output.DryRun {
	if !e.DryRun {
		return nil
	}

	return &output.DryRun{Calls: e.Projection.Calls, Cached: e.Projection.Cached}
}
