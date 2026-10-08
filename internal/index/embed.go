package index

import (
	"context"
	"fmt"
	"sync"

	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"golang.org/x/sync/errgroup"
)

const defaultEmbedConcurrency = 5

type embedJob struct {
	adrs        []ADR
	embedder    inference.Embedder
	concurrency int
	out         *output.Printer
	embedLabel  string
	persist     func(ctx context.Context, adr ADR) error
}

type embedOutcome struct {
	failed  map[int]bool
	skipped []SkippedADR
}

func adrUnchanged(existing, current ADR) bool {
	return existing.Content == current.Content && existing.Title == current.Title && existing.Status == current.Status
}

// A failed ADR never returns an error to the errgroup: that would cancel the embeds still in flight.
func (j embedJob) run(ctx context.Context, indices []int) (embedOutcome, error) {
	outcome := embedOutcome{failed: make(map[int]bool)}
	if len(indices) == 0 {
		return outcome, nil
	}

	concurrency := j.concurrency
	if concurrency <= 0 {
		concurrency = defaultEmbedConcurrency
	}

	var mu sync.Mutex

	group := new(errgroup.Group)
	group.SetLimit(concurrency)
	progress := j.out.Progress()

	markFailed := func(idx int, err error) {
		mu.Lock()
		outcome.failed[idx] = true
		outcome.skipped = append(outcome.skipped, SkippedADR{RelPath: j.adrs[idx].RelPath, Err: err})
		mu.Unlock()
		j.out.Warn("skipping ADR %s: %v", j.adrs[idx].RelPath, err)
	}

	for _, idx := range indices {
		group.Go(func() error {
			if err := j.embedOne(ctx, idx); err != nil {
				markFailed(idx, err)
				return nil
			}

			progress.Tick()

			return nil
		})
	}

	err := group.Wait()
	progress.Done()

	return outcome, err
}

func (j embedJob) embedOne(ctx context.Context, idx int) error {
	adr := j.adrs[idx]
	text := fmt.Sprintf("Title: %s\nStatus: %s\nContent: %s", adr.Title, adr.Status, adr.Content)

	emb, err := j.embedder.CreateEmbedding(ctx, text, inference.EmbeddingTaskDocument)
	if err != nil {
		if j.embedLabel != "" {
			return fmt.Errorf("%s: %w", j.embedLabel, err)
		}

		return err
	}

	j.adrs[idx].Embedding = emb

	if j.persist == nil {
		return nil
	}

	return j.persist(ctx, j.adrs[idx])
}

// Checked even when nothing needed embedding: a cancelled ctx must not pass as success.
func (o embedOutcome) check(ctx context.Context, total int, action string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if total > 0 && len(o.failed) == total {
		return fmt.Errorf("all %d ADR(s) failed to %s; index not updated", total, action)
	}

	return nil
}
