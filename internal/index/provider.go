package index

import (
	"context"
	"fmt"
	"sync"
)

// Provider defines how ArchGuard fetches ADR documents.
type Provider interface {
	// GetADRs fetches ADRs, returning only those that match the provider's criteria.
	GetADRs(ctx context.Context) ([]ADR, error)
}

// CompositeProvider aggregates multiple providers and merges their results.
type CompositeProvider struct {
	providers []Provider
}

// NewCompositeProvider creates a new CompositeProvider with the given providers.
func NewCompositeProvider(providers ...Provider) *CompositeProvider {
	return &CompositeProvider{
		providers: providers,
	}
}

// GetADRs fetches ADRs from all configured providers concurrently and aggregates them into a single slice.
func (c *CompositeProvider) GetADRs(ctx context.Context) ([]ADR, error) {
	var allADRs []ADR
	var errs []error
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range c.providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			adrs, err := p.GetADRs(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				// Do not crash the entire run if one remote provider drops connection.
				fmt.Printf("Warning: failed to fetch ADRs from a provider: %v\n", err)
				errs = append(errs, err)
				return
			}
			allADRs = append(allADRs, adrs...)
		}(p)
	}
	wg.Wait()

	// If every single provider failed, then we should return an error.
	if len(c.providers) > 0 && len(errs) == len(c.providers) {
		return nil, fmt.Errorf("all providers failed to fetch ADRs: %v", errs[0])
	}

	return allADRs, nil
}
