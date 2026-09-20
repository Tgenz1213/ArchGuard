package index

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

type FetchStats struct {
	Discovered     int      // total ADR files/pages found, valid or not
	ParseFailed    []string // paths/IDs that failed to parse (frontmatter/YAML errors)
	StatusRejected int      // count excluded by accepted_statuses filtering
}

func isAcceptedStatus(status string, accepted []string) bool {
	for _, a := range accepted {
		if a == "*" || strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(a)) {
			return true
		}
	}
	return false
}

type Provider interface {
	GetADRs(ctx context.Context) ([]ADR, FetchStats, error)
}

type CompositeProvider struct {
	providers []Provider
	writer    io.Writer
}

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
				diagPrintf(c.writer, "Warning: failed to fetch ADRs from a provider: %v\n", err)
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

	if len(c.providers) > 0 && len(errs) == len(c.providers) {
		return nil, FetchStats{}, fmt.Errorf("all providers failed to fetch ADRs: %v", errs[0])
	}

	return allADRs, stats, nil
}
