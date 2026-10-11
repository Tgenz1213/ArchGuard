package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
)

func (r *checkRun) loadIndex(ctx context.Context, store index.VectorStore) error {
	cfg := r.setup.cfg

	// Held back because a rebuild may fetch the ADRs again and repeat these warnings.
	fetchOut := r.log.Group("")

	validADRs, _, err := newADRProvider(r.setup, fetchOut).GetADRs(ctx)
	if err != nil {
		fetchOut.Flush()
		return fmt.Errorf("failed to fetch ADRs: %w", err)
	}

	currentHash, err := store.CalculateHash(validADRs, cfg.VectorStore.Model)
	if err != nil {
		fetchOut.Flush()
		return fmt.Errorf("failed to calculate index hash: %w", err)
	}

	var skipped []index.SkippedADR

	err = store.Load(r.setup.indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash)
	if err == nil {
		fetchOut.Flush()
	} else {
		r.log.Debug("Saved index does not match: %v", err)

		if skipped, err = r.rebuildIndex(ctx, store, validADRs, staleReason(err)); err != nil {
			return err
		}
	}

	r.unindexed = r.unindexedADRs(store, validADRs, skipped)

	return nil
}

func staleReason(err error) string {
	var reasons []string

	for _, known := range []error{index.ErrIndexMissing, index.ErrEmbeddingModelChanged, index.ErrADRsChanged} {
		if errors.Is(err, known) {
			reasons = append(reasons, known.Error())
		}
	}

	if len(reasons) == 0 {
		return "saved index unreadable"
	}

	return strings.Join(reasons, ", ")
}

func (r *checkRun) rebuildIndex(ctx context.Context, store index.VectorStore, validADRs []index.ADR, reason string) ([]index.SkippedADR, error) {
	cfg := r.setup.cfg

	result, err := buildIndex(ctx, r.setup, r.log.Quiet())
	if result.Attempted {
		r.logRebuild(reason, result)
	}

	if err != nil {
		return nil, fmt.Errorf("index rebuild failed: %w", err)
	}

	currentHash, err := store.CalculateHash(validADRs, cfg.VectorStore.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate rebuilt index hash: %w", err)
	}

	if err := store.Load(r.setup.indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
		return nil, fmt.Errorf("failed to load rebuilt index: %w", err)
	}

	return result.Skipped, nil
}

// Parse failures, malformed rules and failed embeddings are already warned about as they happen.
func (r *checkRun) logRebuild(reason string, result index.BuildIndexResult) {
	r.log.Info("Rebuilt the ADR index (%s): embedded %d ADR(s).", reason, result.Embedded)

	for _, id := range slices.Sorted(maps.Keys(result.DuplicateIDs)) {
		r.log.Warn("ADR ID %q is used by: %s", id, strings.Join(result.DuplicateIDs[id], ", "))
	}
}

func (r *checkRun) unindexedADRs(store index.VectorStore, validADRs []index.ADR, skipped []index.SkippedADR) []output.ADRGap {
	paths, err := store.IndexedPaths()
	if err != nil {
		r.log.Warn("could not list the indexed ADRs, so ADRs missing from the index are not reported: %v", err)
		return nil
	}

	reasons := make(map[string]string, len(skipped))
	for _, adr := range skipped {
		reasons[adr.RelPath] = fmt.Sprintf("failed to embed: %v", adr.Err)
	}

	var gaps []output.ADRGap

	for _, adr := range validADRs {
		if slices.Contains(paths, adr.RelPath) {
			continue
		}

		reason, ok := reasons[adr.RelPath]
		if !ok {
			reason = "not in the index; run `archguard index` to see why"
		}

		gaps = append(gaps, output.ADRGap{ADR: adr.RelPath, Reason: reason})
	}

	return gaps
}
