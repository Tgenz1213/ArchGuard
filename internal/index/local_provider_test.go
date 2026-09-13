package index

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestLocalProvider_CustomIDPatternAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"adr-1-use-postgres.md": "---\ntitle: Use Postgres\nstatus: Accepted\n---\nBody",
		"adr-2-use-kafka.md":    "---\ntitle: Use Kafka\nstatus: Accepted\n---\nBody",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	provider := NewLocalProvider(dir, []string{"Accepted"})
	provider.SetIDPattern(regexp.MustCompile(`^adr-(\d+)-`))

	adrs, stats, err := provider.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("GetADRs failed: %v", err)
	}
	if stats.Discovered != 2 {
		t.Errorf("Discovered = %d, want 2", stats.Discovered)
	}

	ids := map[string]bool{}
	for _, adr := range adrs {
		ids[adr.ID] = true
	}
	if len(ids) != 2 {
		t.Errorf("got %d distinct IDs (%v), want 2", len(ids), ids)
	}
}

func TestLocalProvider_SetWriter_RoutesParseWarningsThere(t *testing.T) {
	dir := t.TempDir()
	writeADRFile(t, dir, "0001-bad.md", "not frontmatter at all")

	var buf bytes.Buffer
	provider := NewLocalProvider(dir, []string{"Accepted"})
	provider.SetWriter(&buf)

	_, _, err := provider.GetADRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "Warning: skipping") {
		t.Errorf("expected the parse-failure warning on the configured writer, got %q", buf.String())
	}
}
