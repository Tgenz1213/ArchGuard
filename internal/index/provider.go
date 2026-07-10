package index

import (
	"context"
	"fmt"
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

// GetADRs fetches ADRs from all configured providers and aggregates them into a single slice.
func (c *CompositeProvider) GetADRs(ctx context.Context) ([]ADR, error) {
	var allADRs []ADR

	for _, p := range c.providers {
		adrs, err := p.GetADRs(ctx)
		if err != nil {
			// Do not crash the entire run if one remote provider drops connection.
			fmt.Printf("Warning: failed to fetch ADRs from a provider: %v\n", err)
			continue
		}
		allADRs = append(allADRs, adrs...)
	}

	return allADRs, nil
}
