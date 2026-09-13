package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeADRFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}

func TestLocalProvider_GetADRs_ReportsFetchStats(t *testing.T) {
	dir := t.TempDir()

	writeADRFile(t, dir, "0001-accepted.md", "---\ntitle: A\nstatus: Accepted\n---\ncontent")
	writeADRFile(t, dir, "0002-rejected.md", "---\ntitle: B\nstatus: Proposed\n---\ncontent")
	writeADRFile(t, dir, "0003-unparseable.md", "not frontmatter at all")

	provider := NewLocalProvider(dir, []string{"Accepted"})
	adrs, stats, err := provider.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adrs) != 1 || adrs[0].RelPath != "0001-accepted.md" {
		t.Fatalf("expected only the accepted ADR, got %+v", adrs)
	}
	if stats.Discovered != 3 {
		t.Errorf("expected 3 discovered .md files, got %d", stats.Discovered)
	}
	if stats.StatusRejected != 1 {
		t.Errorf("expected 1 status-rejected ADR, got %d", stats.StatusRejected)
	}
	if len(stats.ParseFailed) != 1 || filepath.Base(stats.ParseFailed[0]) != "0003-unparseable.md" {
		t.Errorf("expected 0003-unparseable.md reported as parse-failed, got %v", stats.ParseFailed)
	}
}

func TestLocalProvider_GetADRs_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	provider := NewLocalProvider(dir, []string{"Accepted"})
	adrs, stats, err := provider.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adrs) != 0 || stats.Discovered != 0 {
		t.Errorf("expected no ADRs and 0 discovered for an empty directory, got adrs=%+v stats=%+v", adrs, stats)
	}
}
