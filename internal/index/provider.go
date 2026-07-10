package index

import "context"

// Provider defines how ArchGuard fetches ADR documents.
type Provider interface {
	// GetADRs fetches ADRs, returning only those that match the provider's criteria.
	GetADRs(ctx context.Context) ([]ADR, error)
}
