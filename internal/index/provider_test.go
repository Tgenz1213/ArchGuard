package index

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	adrs  []ADR
	stats FetchStats
	err   error
}

func (f *fakeProvider) GetADRs(ctx context.Context) ([]ADR, FetchStats, error) {
	return f.adrs, f.stats, f.err
}

func TestCompositeProvider_GetADRs_MergesStatsAcrossProviders(t *testing.T) {
	p1 := &fakeProvider{
		adrs:  []ADR{{ID: "0001", RelPath: "0001-a.md"}},
		stats: FetchStats{Discovered: 2, ParseFailed: []string{"bad-local.md"}, StatusRejected: 1},
	}
	p2 := &fakeProvider{
		adrs:  []ADR{{ID: "confluence-1", RelPath: "confluence-1"}},
		stats: FetchStats{Discovered: 3, ParseFailed: []string{"bad-confluence"}, StatusRejected: 2},
	}

	composite := NewCompositeProvider(p1, p2)
	adrs, stats, err := composite.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adrs) != 2 {
		t.Fatalf("expected 2 merged ADRs, got %d", len(adrs))
	}
	if stats.Discovered != 5 {
		t.Errorf("expected Discovered to sum to 5, got %d", stats.Discovered)
	}
	if stats.StatusRejected != 3 {
		t.Errorf("expected StatusRejected to sum to 3, got %d", stats.StatusRejected)
	}
	if len(stats.ParseFailed) != 2 {
		t.Errorf("expected ParseFailed to concatenate to 2 entries, got %v", stats.ParseFailed)
	}
}

func TestCompositeProvider_GetADRs_PartialFailureKeepsOtherProviderStats(t *testing.T) {
	ok := &fakeProvider{
		adrs:  []ADR{{ID: "0001", RelPath: "0001-a.md"}},
		stats: FetchStats{Discovered: 1},
	}
	failing := &fakeProvider{err: errors.New("connection dropped")}

	composite := NewCompositeProvider(ok, failing)
	adrs, stats, err := composite.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("expected no error when only one of two providers fails, got: %v", err)
	}

	if len(adrs) != 1 || stats.Discovered != 1 {
		t.Errorf("expected the healthy provider's ADRs and stats to survive, got adrs=%+v stats=%+v", adrs, stats)
	}
}

func TestCompositeProvider_GetADRs_AllProvidersFail(t *testing.T) {
	composite := NewCompositeProvider(
		&fakeProvider{err: errors.New("first failure")},
		&fakeProvider{err: errors.New("second failure")},
	)

	_, _, err := composite.GetADRs(context.Background())
	if err == nil {
		t.Fatal("expected an error when every provider fails")
	}
}
