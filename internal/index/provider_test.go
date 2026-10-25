package index

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
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
		stats: FetchStats{Discovered: 2, ParseFailed: []string{"bad-local.md"}, StatusRejected: 1, MalformedRules: []MalformedRules{{RelPath: "0001-a.md", Reason: "r1"}}},
	}
	p2 := &fakeProvider{
		adrs:  []ADR{{ID: "confluence-1", RelPath: "confluence-1"}},
		stats: FetchStats{Discovered: 3, ParseFailed: []string{"bad-confluence"}, StatusRejected: 2, MalformedRules: []MalformedRules{{RelPath: "confluence-1", Reason: "r2"}}},
	}

	composite := NewCompositeProvider(p1, p2)

	adrs, stats, err := composite.GetADRs(t.Context())
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

	if len(stats.MalformedRules) != 2 {
		t.Errorf("expected MalformedRules to concatenate to 2 entries, got %v", stats.MalformedRules)
	}
}

func TestCompositeProvider_GetADRs_PartialFailureKeepsOtherProviderStats(t *testing.T) {
	ok := &fakeProvider{
		adrs:  []ADR{{ID: "0001", RelPath: "0001-a.md"}},
		stats: FetchStats{Discovered: 1},
	}
	failing := &fakeProvider{err: errors.New("connection dropped")}

	var buf bytes.Buffer
	composite := NewCompositeProvider(ok, failing)
	composite.SetPrinter(output.New(&buf, false))

	adrs, stats, err := composite.GetADRs(t.Context())
	if err != nil {
		t.Fatalf("expected no error when only one of two providers fails, got: %v", err)
	}

	if len(adrs) != 1 || stats.Discovered != 1 {
		t.Errorf("expected the healthy provider's ADRs and stats to survive, got adrs=%+v stats=%+v", adrs, stats)
	}

	if got := strings.Count(buf.String(), "connection dropped"); got != 1 {
		t.Errorf("expected the failure warned once, got %d times in %q", got, buf.String())
	}
}

func TestCompositeProvider_GetADRs_AllProvidersFail(t *testing.T) {
	first := errors.New("first failure")
	second := errors.New("second failure")

	var buf bytes.Buffer
	composite := NewCompositeProvider(&fakeProvider{err: first}, &fakeProvider{err: second})
	composite.SetPrinter(output.New(&buf, false))

	_, _, err := composite.GetADRs(t.Context())
	if err == nil {
		t.Fatal("expected an error when every provider fails")
	}

	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Errorf("expected the error to wrap both failures, got: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no warning when the error is returned, got %q", buf.String())
	}
}

func TestCompositeProvider_SetPrinter_RoutesFetchWarningThere(t *testing.T) {
	ok := &fakeProvider{adrs: []ADR{{RelPath: "a.md"}}}
	failing := &fakeProvider{err: errors.New("connection dropped")}

	var buf bytes.Buffer
	composite := NewCompositeProvider(ok, failing)
	composite.SetPrinter(output.New(&buf, false))

	_, _, err := composite.GetADRs(t.Context())
	if err != nil {
		t.Fatalf("unexpected error (only one of two providers failed): %v", err)
	}

	if buf.Len() == 0 {
		t.Error("expected the fetch warning on the configured writer, got nothing")
	}
}
