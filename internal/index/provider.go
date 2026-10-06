package index

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/tgenz1213/archguard/internal/output"
	"golang.org/x/sync/errgroup"
)

type FetchStats struct {
	Discovered     int              // total ADR files/pages found, valid or not
	ParseFailed    []string         // paths/IDs that failed to parse (frontmatter/YAML errors)
	StatusRejected int              // count excluded by accepted_statuses filtering
	MalformedRules []MalformedRules // accepted ADRs that loaded without their rules
}

type MalformedRules struct {
	RelPath string
	Reason  string
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
	out       *output.Printer
}

func NewCompositeProvider(providers ...Provider) *CompositeProvider {
	return &CompositeProvider{
		providers: providers,
	}
}

func (c *CompositeProvider) SetPrinter(out *output.Printer) {
	c.out = out
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
				// One failing provider must not fail the run.
				c.out.Warn("failed to fetch ADRs from a provider: %v", err)
				errs = append(errs, err)
				return nil
			}

			allADRs = append(allADRs, adrs...)
			stats.Discovered += s.Discovered
			stats.ParseFailed = append(stats.ParseFailed, s.ParseFailed...)
			stats.StatusRejected += s.StatusRejected
			stats.MalformedRules = append(stats.MalformedRules, s.MalformedRules...)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, FetchStats{}, err
	}

	if len(c.providers) > 0 && len(errs) == len(c.providers) {
		return nil, FetchStats{}, fmt.Errorf("all providers failed to fetch ADRs: %w", errs[0])
	}

	return allADRs, stats, nil
}
