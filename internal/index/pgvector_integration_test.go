package index_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

// setupPgContainer starts a pgvector container, returning its connection
// string. Skips the test if Docker isn't available.
func setupPgContainer(tb testing.TB, ctx context.Context) string {
	tb.Helper()

	pgContainer, err := postgres.Run(ctx, "pgvector/pgvector:0.8.6-pg16",
		postgres.WithDatabase("archguard_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		if strings.Contains(err.Error(), "failed to create Docker provider") || strings.Contains(err.Error(), "Cannot connect to the Docker daemon") {
			tb.Skipf("Skipping integration test: Docker is not available on this host (%v)", err)
		}
		require.NoError(tb, err)
	}

	tb.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			tb.Fatalf("failed to terminate container: %s", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(tb, err)
	return connStr
}

// writeADRFiles creates n valid ADR markdown files (adr_0.md .. adr_{n-1}.md) in dir.
func writeADRFiles(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("---\ntitle: \"ADR %d\"\nstatus: \"Accepted\"\n---\nContent %d", i, i)
		err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("adr_%d.md", i)), []byte(content), 0644)
		require.NoError(t, err)
	}
}

// modifyADRFile rewrites adr_{i}.md in dir with different content, so a
// subsequent BuildIndex sees it as a modified (churned) ADR.
func modifyADRFile(t *testing.T, dir string, i int) {
	t.Helper()
	content := fmt.Sprintf("---\ntitle: \"ADR %d\"\nstatus: \"Accepted\"\n---\nModified Content %d", i, i)
	err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("adr_%d.md", i)), []byte(content), 0644)
	require.NoError(t, err)
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	fn()

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}

func mockEmbedProvider() *llm.MockProvider {
	return &llm.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			return []float32{0.1, 0.1}, nil
		},
	}
}

func TestPgStore_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	// 2. Initialize PgStore
	store, err := index.NewPgStore(connStr, "integration_test_project", 5, index.HNSWOptions{})
	require.NoError(t, err)

	// 3. Load Store
	err = store.Load("", "test-model", 2, "")
	require.NoError(t, err)

	// 4. Create Mock ADRs
	tmpDir, err := os.MkdirTemp("", "archguard_integration")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	adrContent := `---
title: "Integration Test ADR"
status: "Accepted"
scope: "**/*.go"
---
Test Content`
	err = os.WriteFile(filepath.Join(tmpDir, "0001-test-adr.md"), []byte(adrContent), 0644)
	require.NoError(t, err)

	// 5. Build Index
	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})
	_, err = store.BuildIndex(ctx, "test-model", 3, provider, localProvider)
	require.NoError(t, err)

	// Insert into a second project to test isolation
	storeOther, err := index.NewPgStore(connStr, "other_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	_, err = storeOther.BuildIndex(ctx, "test-model", 3, provider, localProvider)
	require.NoError(t, err)

	// Same ADR was inserted into two projects; scoping should return only 1.
	results := store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	assert.Len(t, results, 1)
	if len(results) > 0 {
		assert.Equal(t, "Integration Test ADR", results[0].ADR.Title)
		assert.Equal(t, "Accepted", results[0].ADR.Status)
		assert.Contains(t, results[0].ADR.Content, "Test Content")
		assert.Equal(t, "0001", results[0].ADR.ID, "ADR ID (derived from the 0001- filename prefix) should round-trip through PgStore")
		assert.Equal(t, "**/*.go", results[0].ADR.Scope, "ADR scope glob should round-trip through PgStore")
		// Similarity score should be very high
		assert.Greater(t, results[0].Score, 0.9)
	}
}

func TestPgStore_Integration_ReindexDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	disabled := false
	store, err := index.NewPgStore(connStr, "reindex_disabled_project", 5, index.HNSWOptions{Enabled: &disabled})
	require.NoError(t, err)
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	writeADRFiles(t, tmpDir, 5)
	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	// 100% churn, well over the default threshold, but Enabled=false.
	output := captureStdout(t, func() {
		_, err = store.BuildIndex(ctx, "test-model", 3, provider, localProvider)
	})
	require.NoError(t, err)
	assert.NotContains(t, output, "Rebuilding HNSW index")
}

func TestPgStore_Integration_ReindexThresholdRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)
	provider := mockEmbedProvider()

	// High threshold (50%): a later 10% churn should NOT trigger a reindex.
	highTmpDir := t.TempDir()
	writeADRFiles(t, highTmpDir, 10)
	highLocalProvider := index.NewLocalProvider(highTmpDir, []string{"Accepted"})

	highThreshold := 0.5
	storeHigh, err := index.NewPgStore(connStr, "reindex_threshold_high", 5, index.HNSWOptions{Threshold: &highThreshold})
	require.NoError(t, err)
	require.NoError(t, storeHigh.Load("", "test-model", 2, ""))
	// Baseline build: 100% churn (first build), ignored -- only sets up the
	// "existing" state so the next build's churn reflects the real edit below.
	_, err = storeHigh.BuildIndex(ctx, "test-model", 3, provider, highLocalProvider)
	require.NoError(t, err)

	modifyADRFile(t, highTmpDir, 0) // 1 of 10 changed = 10% churn

	outputHigh := captureStdout(t, func() {
		_, err = storeHigh.BuildIndex(ctx, "test-model", 3, provider, highLocalProvider)
	})
	require.NoError(t, err)
	assert.NotContains(t, outputHigh, "Rebuilding HNSW index", "10% churn should not exceed a 50% threshold")

	// Low threshold (5%): the same 10% churn pattern SHOULD trigger a reindex.
	lowTmpDir := t.TempDir()
	writeADRFiles(t, lowTmpDir, 10)
	lowLocalProvider := index.NewLocalProvider(lowTmpDir, []string{"Accepted"})

	lowThreshold := 0.05
	storeLow, err := index.NewPgStore(connStr, "reindex_threshold_low", 5, index.HNSWOptions{Threshold: &lowThreshold})
	require.NoError(t, err)
	require.NoError(t, storeLow.Load("", "test-model", 2, ""))
	_, err = storeLow.BuildIndex(ctx, "test-model", 3, provider, lowLocalProvider)
	require.NoError(t, err)

	modifyADRFile(t, lowTmpDir, 0)

	outputLow := captureStdout(t, func() {
		_, err = storeLow.BuildIndex(ctx, "test-model", 3, provider, lowLocalProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, outputLow, "Rebuilding HNSW index", "10% churn should exceed a 5% threshold")
	assert.NotContains(t, outputLow, "Warning: failed to reindex", "the triggered reindex should actually succeed, not just be attempted")
}

func TestPgStore_Integration_ReindexConcurrentlyConfigured(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)
	provider := mockEmbedProvider()

	// Default (Concurrently unset): should log the "(concurrently)" mode.
	defaultTmpDir := t.TempDir()
	writeADRFiles(t, defaultTmpDir, 3)
	defaultLocalProvider := index.NewLocalProvider(defaultTmpDir, []string{"Accepted"})

	storeDefault, err := index.NewPgStore(connStr, "reindex_concurrently_default", 5, index.HNSWOptions{})
	require.NoError(t, err)
	require.NoError(t, storeDefault.Load("", "test-model", 2, ""))

	outputDefault := captureStdout(t, func() {
		_, err = storeDefault.BuildIndex(ctx, "test-model", 3, provider, defaultLocalProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, outputDefault, "Rebuilding HNSW index (concurrently)", "default should use the non-blocking CONCURRENTLY form")
	assert.NotContains(t, outputDefault, "Warning: failed to reindex", "REINDEX INDEX CONCURRENTLY should actually succeed against a real pgvector HNSW index")

	// Explicit false: should log the "(blocking)" mode instead.
	blocking := false
	blockingTmpDir := t.TempDir()
	writeADRFiles(t, blockingTmpDir, 3)
	blockingLocalProvider := index.NewLocalProvider(blockingTmpDir, []string{"Accepted"})

	storeBlocking, err := index.NewPgStore(connStr, "reindex_concurrently_blocking", 5, index.HNSWOptions{Concurrently: &blocking})
	require.NoError(t, err)
	require.NoError(t, storeBlocking.Load("", "test-model", 2, ""))

	outputBlocking := captureStdout(t, func() {
		_, err = storeBlocking.BuildIndex(ctx, "test-model", 3, provider, blockingLocalProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, outputBlocking, "Rebuilding HNSW index (blocking)", "explicit false should use the blocking REINDEX form")
	assert.NotContains(t, outputBlocking, "(concurrently)")
	assert.NotContains(t, outputBlocking, "Warning: failed to reindex", "the blocking REINDEX INDEX form should actually succeed too")
}

// showIterativeScan reads it from store's own pool, reflecting the
// AfterConnect-applied session state rather than a fresh connection's default.
func showIterativeScan(t *testing.T, ctx context.Context, store *index.PgStore) string {
	t.Helper()

	conn, err := store.Pool().Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	var value string
	require.NoError(t, conn.QueryRow(ctx, "SHOW hnsw.iterative_scan").Scan(&value))
	return value
}

func TestPgStore_Integration_IterativeScanDefaultEnabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "iterative_scan_default_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	// Independent probe, not reused from NewPgStore, so the test can self-skip.
	probeConn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	var pgvectorVersion string
	err = probeConn.QueryRow(ctx, index.PgvectorVersionQuery).Scan(&pgvectorVersion)
	_ = probeConn.Close(ctx)
	require.NoError(t, err)
	if !index.IterativeScanSupportedVersion(pgvectorVersion) {
		t.Skipf("pgvector %s does not support hnsw.iterative_scan (requires 0.8.0+)", pgvectorVersion)
	}

	value := showIterativeScan(t, ctx, store)
	t.Logf("SHOW hnsw.iterative_scan (default) = %q", value)
	assert.Equal(t, "relaxed_order", value, "default HNSWOptions should apply hnsw.iterative_scan = 'relaxed_order' on this pgvector version")
}

func TestPgStore_Integration_IterativeScanExplicitlyDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	disabled := false
	store, err := index.NewPgStore(connStr, "iterative_scan_disabled_project", 5, index.HNSWOptions{IterativeScan: &disabled})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	value := showIterativeScan(t, ctx, store)
	t.Logf("SHOW hnsw.iterative_scan (explicitly disabled) = %q", value)
	assert.Equal(t, "off", value, "IterativeScan: false should leave hnsw.iterative_scan at pgvector's default value")
}

func TestPgStore_Integration_SyncsMetadataForUnchangedADR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "sync_metadata_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	// Simulate a legacy row with adr_id/scope still NULL.
	_, err = store.Pool().Exec(ctx, `
		INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, "sync_metadata_project", "0007-legacy.md", "Legacy ADR", "Accepted", "\nLegacy Content", pgvector.NewVector([]float32{0.1, 0.1}))
	require.NoError(t, err)

	preSyncResults := store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	require.Len(t, preSyncResults, 1, "Search must still find a row with NULL adr_id/scope columns, not fail the scan")
	assert.Equal(t, "", preSyncResults[0].ADR.ID, "NULL adr_id should degrade to empty string via COALESCE, not break the scan")
	assert.Equal(t, "", preSyncResults[0].ADR.Scope, "NULL scope should degrade to empty string via COALESCE, not break the scan")

	tmpDir := t.TempDir()
	adrContent := "---\ntitle: \"Legacy ADR\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\nLegacy Content"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "0007-legacy.md"), []byte(adrContent), 0644))

	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	output := captureStdout(t, func() {
		_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Generating embeddings for 0 new/modified ADRs", "content/title/status are unchanged, so this must NOT re-embed")
	assert.Contains(t, output, "Syncing ID/scope metadata for 1 unchanged ADR", "the ID/scope mismatch must still trigger the lightweight sync path")

	results := store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	require.Len(t, results, 1)
	assert.Equal(t, "0007", results[0].ADR.ID, "adr_id should be backfilled from NULL by the sync path")
	assert.Equal(t, "**/*.go", results[0].ADR.Scope, "scope should be backfilled from NULL by the sync path")
}

// TestPgStore_Integration_SyncsMetadataForScopeOnlyEdit covers the other
// adrsToSync trigger: a scope-only edit, not a legacy NULL row.
func TestPgStore_Integration_SyncsMetadataForScopeOnlyEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "scope_only_edit_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	adrPath := filepath.Join(tmpDir, "0008-scope-edit.md")
	initialContent := "---\ntitle: \"Scope Edit ADR\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\nBody unchanged."
	require.NoError(t, os.WriteFile(adrPath, []byte(initialContent), 0644))

	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	require.NoError(t, err)

	results := store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	require.Len(t, results, 1)
	assert.Equal(t, "**/*.go", results[0].ADR.Scope, "initial index should embed the original scope")

	// Same title/status/body -- only the scope: frontmatter value changes.
	editedContent := "---\ntitle: \"Scope Edit ADR\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nBody unchanged."
	require.NoError(t, os.WriteFile(adrPath, []byte(editedContent), 0644))

	output := captureStdout(t, func() {
		_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Generating embeddings for 0 new/modified ADRs", "content/title/status are unchanged, so this must NOT re-embed")
	assert.Contains(t, output, "Syncing ID/scope metadata for 1 unchanged ADR", "the scope-only change must route through the sync path")

	results = store.Search([]float32{0.1, 0.1}, 0.5, 5, "app.ts")
	require.Len(t, results, 1)
	assert.Equal(t, "**/*.ts", results[0].ADR.Scope, "sync path must pick up the new scope value")
}

// A single failing embed call must not abort the whole build (#133).
func TestPgStore_Integration_BuildIndexSkipsFailedADRAndContinuesEmbeddingOthers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "embed_failure_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	writeADRFiles(t, tmpDir, 3)
	// writeADRFiles names files adr_0.md, adr_1.md, adr_2.md with titles
	// "ADR 0"/"ADR 1"/"ADR 2" -- make adr_1 the one that fails to embed.
	provider := &llm.MockProvider{
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "Title: ADR 1") {
				return nil, fmt.Errorf("simulated embedding failure")
			}
			return []float32{0.1, 0.1}, nil
		},
	}
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	var result index.BuildIndexResult
	output := captureStdout(t, func() {
		result, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	})
	require.NoError(t, err, "a single ADR embed failure must not fail the whole build")
	assert.Contains(t, output, "Warning: skipping ADR adr_1.md")

	require.Len(t, result.Skipped, 1)
	assert.Equal(t, "adr_1.md", result.Skipped[0].RelPath)
	assert.Contains(t, result.Skipped[0].Err.Error(), "simulated embedding failure")

	results := store.Search([]float32{0.1, 0.1}, 0.5, 10, "main.go")
	gotPaths := make(map[string]bool)
	for _, r := range results {
		gotPaths[r.ADR.RelPath] = true
	}
	assert.True(t, gotPaths["adr_0.md"], "adr_0.md must still be embedded and searchable")
	assert.True(t, gotPaths["adr_2.md"], "adr_2.md must still be embedded and searchable")
	assert.False(t, gotPaths["adr_1.md"], "adr_1.md must not appear -- its embed failed, so it was never inserted")
}

// A failed re-embed must leave the ADR's already-indexed row as it was --
// not update it to the new content, and not delete it (#133).
func TestPgStore_Integration_BuildIndexLeavesExistingRowUntouchedOnReEmbedFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "reembed_failure_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	adrPath := filepath.Join(tmpDir, "0010-reembed.md")
	originalBody := "Original indexed body."
	originalContent := "---\ntitle: \"Re-embed ADR\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\n" + originalBody
	require.NoError(t, os.WriteFile(adrPath, []byte(originalContent), 0644))

	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	_, err = store.BuildIndex(ctx, "test-model", 2, mockEmbedProvider(), localProvider)
	require.NoError(t, err, "the first build must succeed and persist the row")

	results := store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	require.Len(t, results, 1)
	require.Contains(t, results[0].ADR.Content, originalBody)

	newBody := "Rewritten body that cannot be embedded."
	editedContent := "---\ntitle: \"Re-embed ADR\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\n" + newBody
	require.NoError(t, os.WriteFile(adrPath, []byte(editedContent), 0644))

	failingProvider := &llm.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			return nil, fmt.Errorf("simulated re-embed failure")
		},
	}

	var result index.BuildIndexResult
	output := captureStdout(t, func() {
		result, err = store.BuildIndex(ctx, "test-model", 2, failingProvider, localProvider)
	})
	// This ADR is the whole corpus, so the all-embeds-failed guard fires --
	// the point is that it fires WITHOUT having touched the existing row.
	require.Error(t, err, "every ADR failing to embed must surface as a build-wide error")
	assert.Contains(t, err.Error(), "index not updated")
	assert.Contains(t, output, "Warning: skipping ADR 0010-reembed.md")
	require.Len(t, result.Skipped, 1, "the guard must still report what it skipped")
	assert.Equal(t, "0010-reembed.md", result.Skipped[0].RelPath)
	assert.Contains(t, result.Skipped[0].Err.Error(), "embed: ")

	var gotContent string
	var gotEmbedding pgvector.Vector
	err = store.Pool().QueryRow(ctx,
		"SELECT content, embedding FROM archguard_adrs WHERE project_name = $1 AND rel_path = $2",
		"reembed_failure_project", "0010-reembed.md",
	).Scan(&gotContent, &gotEmbedding)
	require.NoError(t, err, "the pre-existing row must still be present, not deleted")
	assert.Contains(t, gotContent, originalBody, "the row must keep its original content")
	assert.NotContains(t, gotContent, newBody, "the failed re-embed must not have written the new content")
	assert.Equal(t, []float32{0.1, 0.1}, gotEmbedding.Slice(), "the row must keep its original embedding")

	results = store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	require.Len(t, results, 1, "the original row must still be searchable")
	assert.Contains(t, results[0].ADR.Content, originalBody)
}

// TestPgStore_Integration_BuildIndexMigratesLegacyTableWithoutLoad reproduces
// cli.runIndex's call pattern (BuildIndex with no preceding Load) against a
// table created with the pre-migration schema, to prove BuildIndex can bring
// its own schema up to date rather than depending on Load having run first.
func TestPgStore_Integration_BuildIndexMigratesLegacyTableWithoutLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "legacy_no_load_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()

	// Reproduce an existing PgStore user's pre-upgrade table: the original
	// schema, no adr_id/scope columns, with real indexed data already in it.
	_, err = store.Pool().Exec(ctx, `
		CREATE TABLE IF NOT EXISTS archguard_adrs (
			id SERIAL PRIMARY KEY,
			project_name TEXT NOT NULL DEFAULT 'default',
			rel_path TEXT,
			title TEXT,
			status TEXT,
			content TEXT,
			embedding vector(2),
			UNIQUE (project_name, rel_path)
		)
	`)
	require.NoError(t, err)
	_, err = store.Pool().Exec(ctx, `
		INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, "legacy_no_load_project", "0009-legacy.md", "Legacy Pre-Migration ADR", "Accepted", "\nLegacy Pre-Migration Content", pgvector.NewVector([]float32{0.1, 0.1}))
	require.NoError(t, err)

	tmpDir := t.TempDir()
	adrContent := "---\ntitle: \"Legacy Pre-Migration ADR\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\nLegacy Pre-Migration Content"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "0009-legacy.md"), []byte(adrContent), 0644))

	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	// Deliberately skip store.Load() -- this is what broke before BuildIndex
	// started ensuring its own schema (cli.runIndex never calls Load).
	_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	require.NoError(t, err, "BuildIndex must create/alter its own schema when Load was never called")

	results := store.Search([]float32{0.1, 0.1}, 0.5, 5, "main.go")
	require.Len(t, results, 1)
	assert.Equal(t, "0009", results[0].ADR.ID)
	assert.Equal(t, "**/*.go", results[0].ADR.Scope)
}

func TestPgStore_Integration_BuildIndexReturnsErrorOnScanFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "scan_failure_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	// A valid row plus one with a NULL title (a nullable TEXT column),
	// proving the failure amid several rows, not a single-row table.
	_, err = store.Pool().Exec(ctx, `
		INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, "scan_failure_project", "0002-good.md", "Good ADR", "Accepted", "other content", pgvector.NewVector([]float32{0.1, 0.1}))
	require.NoError(t, err)
	_, err = store.Pool().Exec(ctx, `
		INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
		VALUES ($1, $2, NULL, $3, $4, $5)
	`, "scan_failure_project", "0001-bad.md", "Accepted", "content", pgvector.NewVector([]float32{0.1, 0.1}))
	require.NoError(t, err)

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "0001-bad.md"), []byte("---\ntitle: \"Bad ADR\"\nstatus: \"Accepted\"\n---\ncontent"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "0002-good.md"), []byte("---\ntitle: \"Good ADR\"\nstatus: \"Accepted\"\n---\nother content"), 0644))

	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	require.Error(t, err, "a Scan failure on one row must fail BuildIndex, not be silently dropped")
	assert.Contains(t, err.Error(), "failed to scan existing ADR row")
}

// fakeContentProvider is a minimal analysis.ContentProvider for exercising
// Engine.Run against a real PgStore without needing git plumbing.
type fakeContentProvider struct {
	files map[string]string
}

func (f *fakeContentProvider) GetFiles() ([]string, error) {
	names := make([]string, 0, len(f.files))
	for name := range f.files {
		names = append(names, name)
	}
	return names, nil
}

func (f *fakeContentProvider) GetContent(path string) (string, error) {
	return f.files[path], nil
}

func (f *fakeContentProvider) GetDiff(path string) (string, error) {
	return "", nil
}

// buildTwoADREngineFixture indexes two broadly-matching ADRs ("0001-a.md",
// "0002-b.md") into a fresh PgStore-backed project and returns an Engine
// wired to it, a MockProvider that always reports a violation, and the
// project name -- shared setup for the archguard-ignore and baseline
// scoping tests below, which both need two ADRs relevant to the same file
// so suppressing one doesn't accidentally suppress the other.
func buildTwoADREngineFixture(t *testing.T, ctx context.Context, connStr, projectName, fileContent string) (*analysis.Engine, *index.PgStore) {
	t.Helper()

	store, err := index.NewPgStore(connStr, projectName, 5, index.HNSWOptions{})
	require.NoError(t, err)
	require.NoError(t, store.Load("", "test-model", 2, ""))

	adrDir := t.TempDir()
	adrA := "---\ntitle: \"ADR A\"\nstatus: \"Accepted\"\n---\nRule A body"
	adrB := "---\ntitle: \"ADR B\"\nstatus: \"Accepted\"\n---\nRule B body"
	require.NoError(t, os.WriteFile(filepath.Join(adrDir, "0001-a.md"), []byte(adrA), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(adrDir, "0002-b.md"), []byte(adrB), 0644))

	llmProvider := &llm.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			return []float32{0.1, 0.1}, nil
		},
		ChatFunc: func(ctx context.Context, system, user string) (string, error) {
			return `{"violation": true, "reasoning": "violates", "quoted_code": "bad code"}`, nil
		},
	}

	adrProvider := index.NewLocalProvider(adrDir, []string{"Accepted"})
	_, err = store.BuildIndex(ctx, "test-model", 2, llmProvider, adrProvider)
	require.NoError(t, err)

	content := &fakeContentProvider{files: map[string]string{"service.go": fileContent}}
	cfg := &config.Config{
		VectorStore: config.VectorStore{SimilarityThreshold: 0.0},
		Analysis:    config.Analysis{ExcludePatterns: []string{}},
	}

	engine := analysis.NewEngine(cfg, store, llmProvider, content, false, false)
	engine.Cache = nil
	return engine, store
}

// TestPgStore_Integration_EngineArchguardIgnoreSuppressesOnlyNamedADR proves
// archguard-ignore against a PgStore-backed index suppresses only the named
// ADR. Before PgStore persisted adr_id, every hit's ADR.ID was "", collapsing
// the "archguard-ignore: %s" match to a bare "archguard-ignore: " substring
// check -- which would suppress both ADRs here, not just the named one.
func TestPgStore_Integration_EngineArchguardIgnoreSuppressesOnlyNamedADR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	engine, store := buildTwoADREngineFixture(t, ctx, connStr, "engine_ignore_project", "archguard-ignore: 0001\nbad code\n")
	defer store.Close()

	err := engine.Run(ctx)
	require.Error(t, err)
	var driftErr *analysis.DriftDetectedError
	require.ErrorAs(t, err, &driftErr)
	assert.Equal(t, 1, driftErr.Count, "ADR A (0001) should be suppressed by archguard-ignore while ADR B (0002) still surfaces as a violation")
}

// TestPgStore_Integration_EngineBaselineSuppressesOnlyNamedADR proves
// baseline suppression against a PgStore-backed index suppresses only the
// ADR named in the baseline entry, not every ADR on the file. Before
// PgStore persisted adr_id, every hit's ADR.ID was "", so a baseline entry
// meant for one ADR would key-match every ADR on that file.
func TestPgStore_Integration_EngineBaselineSuppressesOnlyNamedADR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	engine, store := buildTwoADREngineFixture(t, ctx, connStr, "engine_baseline_project", "bad code\n")
	defer store.Close()

	b := baseline.New()
	b.Add("0001", "service.go", "")
	engine.Baseline = b

	err := engine.Run(ctx)
	require.Error(t, err)
	var driftErr *analysis.DriftDetectedError
	require.ErrorAs(t, err, &driftErr)
	assert.Equal(t, 1, driftErr.Count, "ADR A (0001) should be baselined while ADR B (0002) still surfaces as a new violation")
}

// mirrors search_test.go's LocalStore regression test for #134 against a
// real PgStore -- see that test for the scope-before-topK rationale.
func TestPgStore_Integration_SearchScopeMatchingADRSurvivesDespiteLowerSimilarity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "scope_before_topk_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	adrs := map[string]string{
		"0001-distractor-a.md": "---\ntitle: \"Distractor A\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nDistractor A body",
		"0002-distractor-b.md": "---\ntitle: \"Distractor B\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nDistractor B body",
		"0003-distractor-c.md": "---\ntitle: \"Distractor C\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nDistractor C body",
		"0004-scope-match.md":  "---\ntitle: \"Scope Match\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\nScope Match body",
	}
	for name, content := range adrs {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644))
	}

	// "Scope Match" embeds a less-aligned vector (~0.707 similarity) than
	// the distractors (1.0), but it's the only one scoped to "service.go".
	provider := &llm.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "Scope Match") {
				return []float32{1, 1}, nil
			}
			return []float32{1, 0}, nil
		},
	}
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})
	_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	require.NoError(t, err)

	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	require.Len(t, results, 1, "expected exactly 1 result (the scope-matching ADR): %+v", results)
	assert.Equal(t, "Scope Match", results[0].ADR.Title)
}

// mirrors the topK-vs-scope regression above, but for threshold-vs-scope:
// scope filtering must see every candidate before threshold is applied.
func TestPgStore_Integration_SearchScopeMatchingADRSurvivesDespiteBelowThresholdSimilarity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "threshold_after_scope_project", 5, index.HNSWOptions{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	adrs := map[string]string{
		"0001-distractor-a.md": "---\ntitle: \"Distractor A\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nDistractor A body",
		"0002-distractor-b.md": "---\ntitle: \"Distractor B\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nDistractor B body",
		"0003-distractor-c.md": "---\ntitle: \"Distractor C\"\nstatus: \"Accepted\"\nscope: \"**/*.ts\"\n---\nDistractor C body",
		"0004-scope-match.md":  "---\ntitle: \"Scope Match\"\nstatus: \"Accepted\"\nscope: \"**/*.go\"\n---\nScope Match body",
	}
	for name, content := range adrs {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644))
	}

	// "Scope Match" is orthogonal to the query (0.0 similarity, below the 0.5
	// threshold), but it's the only ADR scoped to "service.go" -- a SQL-level threshold predicate would've dropped it before scope filtering ever ran.
	provider := &llm.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string, task llm.EmbeddingTaskType) ([]float32, error) {
			if strings.Contains(text, "Scope Match") {
				return []float32{0, 1}, nil
			}
			return []float32{1, 0}, nil
		},
	}
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})
	_, err = store.BuildIndex(ctx, "test-model", 2, provider, localProvider)
	require.NoError(t, err)

	results := store.Search([]float32{1, 0}, 0.5, 3, "service.go")

	require.Len(t, results, 0, "expected 0 results: Scope Match is a candidate (fetched, scope-matched) but still below threshold, got %+v", results)

	distractorResults := store.Search([]float32{1, 0}, 0.5, 3, "app.ts")
	require.Len(t, distractorResults, 3, "expected the 3 distractors (scoped to **/*.ts, similarity 1.0) for a matching file, got %+v", distractorResults)
}

func TestSearchQuery_HasNoDistanceThresholdPredicate(t *testing.T) {
	if strings.Contains(index.SearchQuery, "<= $") {
		t.Fatalf("SearchQuery still has a SQL-level distance-threshold predicate; scope/threshold filtering must happen in Go, not SQL -- query:\n%s", index.SearchQuery)
	}
}
