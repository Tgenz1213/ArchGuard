package index

import (
	"context"
	"errors"
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

// Gives every provider the same parse, status and malformed-rules bookkeeping.
type adrCollector struct {
	out              *output.Printer
	acceptedStatuses []string
	adrs             []ADR
	stats            FetchStats
}

// label names the ADR in warnings; failedPath is what stats.ParseFailed records.
func (c *adrCollector) add(adr *ADR, rulesErr, parseErr error, label, failedPath string) {
	if parseErr != nil {
		c.out.Warn("skipping %s: %v", label, parseErr)
		c.stats.ParseFailed = append(c.stats.ParseFailed, failedPath)

		return
	}

	if !isAcceptedStatus(adr.Status, c.acceptedStatuses) {
		c.stats.StatusRejected++

		return
	}

	c.adrs = append(c.adrs, *adr)

	if rulesErr != nil {
		c.out.Warn("ignoring rules in %s: %v", label, rulesErr)
		c.stats.MalformedRules = append(c.stats.MalformedRules, MalformedRules{RelPath: adr.RelPath, Reason: rulesErr.Error()})
	}
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
	var fetchGroup errgroup.Group

	for _, provider := range c.providers {
		fetchGroup.Go(func() error {
			adrs, s, err := provider.GetADRs(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
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

	if err := fetchGroup.Wait(); err != nil {
		return nil, FetchStats{}, err
	}

	if len(c.providers) > 0 && len(errs) == len(c.providers) {
		return nil, FetchStats{}, fmt.Errorf("all providers failed to fetch ADRs: %w", errors.Join(errs...))
	}

	// One failing provider must not fail the run.
	for _, err := range errs {
		c.out.Warn("failed to fetch ADRs from a provider: %v", err)
	}

	return allADRs, stats, nil
}
