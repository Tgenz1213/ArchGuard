package index

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// FetchStats summarizes what a Provider encountered while fetching ADRs,
// beyond the valid ADRs it returns from GetADRs.
type FetchStats struct {
	Discovered     int      // total ADR files/pages found, valid or not
	ParseFailed    []string // paths/IDs that failed to parse (frontmatter/YAML errors)
	StatusRejected int      // count excluded by accepted_statuses filtering
}

// isAcceptedStatus reports whether status matches one of accepted (case-insensitive), or accepted contains "*".
func isAcceptedStatus(status string, accepted []string) bool {
	for _, a := range accepted {
		if a == "*" || strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(a)) {
			return true
		}
	}
	return false
}

// Provider defines how ArchGuard fetches ADR documents.
type Provider interface {
	// GetADRs fetches ADRs, returning only those that match the provider's criteria,
	// plus stats on what else it found along the way.
	GetADRs(ctx context.Context) ([]ADR, FetchStats, error)
}

// CompositeProvider aggregates multiple providers and merges their results.
type CompositeProvider struct {
	providers []Provider
	writer    io.Writer
}

// NewCompositeProvider creates a new CompositeProvider with the given providers.
func NewCompositeProvider(providers ...Provider) *CompositeProvider {
	return &CompositeProvider{
		providers: providers,
	}
}

// SetWriter routes GetADRs' provider-fetch warnings to w instead of the
// default os.Stdout. Passing nil restores the default.
func (c *CompositeProvider) SetWriter(w io.Writer) {
	c.writer = w
}

// GetADRs fetches ADRs from all configured providers concurrently and aggregates them into a single slice.
func (c *CompositeProvider) GetADRs(ctx context.Context) ([]ADR, FetchStats, error) {
	var allADRs []ADR
	var stats FetchStats
	var errs []error
	var mu sync.Mutex
	var g errgroup.Group

	for _, p := range c.providers {
		p := p
		g.Go(func() error {
			adrs, s, err := p.GetADRs(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				// Do not crash the entire run if one remote provider drops connection.
				_, _ = fmt.Fprintf(diagWriter(c.writer), "Warning: failed to fetch ADRs from a provider: %v\n", err)
				errs = append(errs, err)
				return nil
			}
			allADRs = append(allADRs, adrs...)
			stats.Discovered += s.Discovered
			stats.ParseFailed = append(stats.ParseFailed, s.ParseFailed...)
			stats.StatusRejected += s.StatusRejected
			return nil
		})
	}
	_ = g.Wait()

	// If every single provider failed, then we should return an error.
	if len(c.providers) > 0 && len(errs) == len(c.providers) {
		return nil, FetchStats{}, fmt.Errorf("all providers failed to fetch ADRs: %v", errs[0])
	}

	return allADRs, stats, nil
}
