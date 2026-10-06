package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
)

// Separate from runIndex, which check's auto-rebuild calls with its own printer.
func runIndexCommand(ctx context.Context, setup runSetup, colors streamColors) (ExitCode, error) {
	out := colors.stdoutPrinter(false)

	code, err := runIndex(ctx, setup, out)
	if werr := out.Err(); werr != nil && code != ExitInterrupted {
		return ExitError, errors.Join(outputWriteError(werr), err)
	}

	return code, err
}

func runIndex(ctx context.Context, setup runSetup, out *output.Printer) (ExitCode, error) {
	cfg := setup.cfg

	store, err := index.NewVectorStore(cfg, out)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	localProvider := index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses)
	localProvider.SetIDPattern(setup.adrIDPattern)
	localProvider.SetFrontmatterMappings(setup.frontmatterMappings)
	localProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
	localProvider.SetPrinter(out)
	var providers []index.Provider
	providers = append(providers, localProvider)

	if cfg.Analysis.Confluence.Enabled {
		confluenceProvider := index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		)
		confluenceProvider.SetFrontmatterMappings(setup.frontmatterMappings)
		confluenceProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
		confluenceProvider.SetPrinter(out)
		providers = append(providers, confluenceProvider)
	}

	adrProvider := index.NewCompositeProvider(providers...)
	adrProvider.SetPrinter(out)

	result, err := store.BuildIndex(ctx, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, setup.embed, adrProvider)
	if result.Attempted {
		printIndexSummary(result, out)
	}

	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to build index: %w", err)
	}

	// Checked before Save so a failed rebuild leaves the prior index intact.
	if result.IsEmpty() {
		return ExitIndexError, fmt.Errorf("no valid ADRs found among %d discovered; index not updated", result.Discovered)
	}

	if err := store.Save(setup.indexFile); err != nil {
		return ExitIndexError, fmt.Errorf("failed to save index: %w", err)
	}

	return ExitSuccess, nil
}

func printIndexSummary(result index.BuildIndexResult, out *output.Printer) {
	section := out.Indented()
	item := section.Indented()

	out.Result("ADR Index: %d discovered, %d valid.", result.Discovered, result.Valid)

	if len(result.ParseFailed) > 0 {
		section.Result("Skipped (parse failure): %d", len(result.ParseFailed))
		for _, path := range result.ParseFailed {
			item.Result("- %s", path)
		}
	}

	if result.StatusRejected > 0 {
		section.Result("Skipped (status not accepted): %d", result.StatusRejected)
	}

	if len(result.Skipped) > 0 {
		section.Result("Failed to embed or persist: %d", len(result.Skipped))
		for _, skipped := range result.Skipped {
			item.Result("- %s: %v", skipped.RelPath, skipped.Err)
		}
	}

	if len(result.DuplicateIDs) > 0 {
		ids := make([]string, 0, len(result.DuplicateIDs))
		for id := range result.DuplicateIDs {
			ids = append(ids, id)
		}

		sort.Strings(ids)
		section.Result("Duplicate ADR IDs: %d", len(ids))
		for _, id := range ids {
			item.Result("- %q used by: %s", id, strings.Join(result.DuplicateIDs[id], ", "))
		}
	}

	if len(result.NoScope) > 0 {
		section.Result("No scope set (applies to every file): %d", len(result.NoScope))
		for _, path := range result.NoScope {
			item.Result("- %s", path)
		}
	}

	if len(result.MalformedRules) > 0 {
		section.Result("Rules ignored (malformed): %d", len(result.MalformedRules))
		for _, m := range result.MalformedRules {
			item.Result("- %s: %s", m.RelPath, m.Reason)
		}
	}
}
