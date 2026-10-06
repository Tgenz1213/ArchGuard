package cli

import (
	"context"
	"fmt"

	"github.com/tgenz1213/archguard/internal/index"
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

	err = store.Load(r.setup.indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash)
	if err == nil {
		fetchOut.Flush()
		return nil
	}

	r.log.Info("Index metadata mismatch or missing index. Triggering index rebuild: %v", err)

	return r.rebuildIndex(ctx, store, validADRs)
}

func (r *checkRun) rebuildIndex(ctx context.Context, store index.VectorStore, validADRs []index.ADR) error {
	cfg := r.setup.cfg

	if _, err := runIndex(ctx, r.setup, r.log); err != nil {
		return fmt.Errorf("index rebuild failed: %w", err)
	}

	currentHash, err := store.CalculateHash(validADRs, cfg.VectorStore.Model)
	if err != nil {
		return fmt.Errorf("failed to calculate rebuilt index hash: %w", err)
	}

	if err := store.Load(r.setup.indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
		return fmt.Errorf("failed to load rebuilt index: %w", err)
	}

	return nil
}
