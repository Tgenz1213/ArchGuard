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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

// setupPgContainer starts a pgvector/pgvector:pg16 container and returns its
// connection string, registering cleanup via t.Cleanup. Skips the test if
// Docker isn't available on the host.
func setupPgContainer(tb testing.TB, ctx context.Context) string {
	tb.Helper()

	pgContainer, err := postgres.Run(ctx, "pgvector/pgvector:pg16",
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
	store, err := index.NewPgStore(connStr, "integration_test_project", 5, index.ReindexOptions{})
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
---
Test Content`
	err = os.WriteFile(filepath.Join(tmpDir, "test_adr.md"), []byte(adrContent), 0644)
	require.NoError(t, err)

	// 5. Build Index
	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})
	err = store.BuildIndex(ctx, "test-model", 3, provider, localProvider)
	require.NoError(t, err)

	// Insert into a second project to test isolation
	storeOther, err := index.NewPgStore(connStr, "other_project", 5, index.ReindexOptions{})
	require.NoError(t, err)
	err = storeOther.BuildIndex(ctx, "test-model", 3, provider, localProvider)
	require.NoError(t, err)

	// 6. Search
	// Query embedding [0.1, 0.1] should match perfectly.
	// Since we inserted the same ADR into two different projects,
	// if scoping works, we should only get 1 result back from the first store, not 2.
	results := store.Search([]float32{0.1, 0.1}, 0.5, 5)
	assert.Len(t, results, 1)
	if len(results) > 0 {
		assert.Equal(t, "Integration Test ADR", results[0].ADR.Title)
		assert.Equal(t, "Accepted", results[0].ADR.Status)
		assert.Contains(t, results[0].ADR.Content, "Test Content")
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
	store, err := index.NewPgStore(connStr, "reindex_disabled_project", 5, index.ReindexOptions{Enabled: &disabled})
	require.NoError(t, err)
	require.NoError(t, store.Load("", "test-model", 2, ""))

	tmpDir := t.TempDir()
	writeADRFiles(t, tmpDir, 5)
	provider := mockEmbedProvider()
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})

	// First build: 5 new / 5 total = 100% churn, comfortably over the
	// default 20% threshold. If Enabled=false is respected, no reindex
	// message is printed despite churn exceeding the threshold.
	output := captureStdout(t, func() {
		err = store.BuildIndex(ctx, "test-model", 3, provider, localProvider)
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
	storeHigh, err := index.NewPgStore(connStr, "reindex_threshold_high", 5, index.ReindexOptions{Threshold: &highThreshold})
	require.NoError(t, err)
	require.NoError(t, storeHigh.Load("", "test-model", 2, ""))
	// Baseline build: 100% churn (first build), ignored -- only sets up the
	// "existing" state so the next build's churn reflects the real edit below.
	require.NoError(t, storeHigh.BuildIndex(ctx, "test-model", 3, provider, highLocalProvider))

	modifyADRFile(t, highTmpDir, 0) // 1 of 10 changed = 10% churn

	outputHigh := captureStdout(t, func() {
		err = storeHigh.BuildIndex(ctx, "test-model", 3, provider, highLocalProvider)
	})
	require.NoError(t, err)
	assert.NotContains(t, outputHigh, "Rebuilding HNSW index", "10% churn should not exceed a 50% threshold")

	// Low threshold (5%): the same 10% churn pattern SHOULD trigger a reindex.
	lowTmpDir := t.TempDir()
	writeADRFiles(t, lowTmpDir, 10)
	lowLocalProvider := index.NewLocalProvider(lowTmpDir, []string{"Accepted"})

	lowThreshold := 0.05
	storeLow, err := index.NewPgStore(connStr, "reindex_threshold_low", 5, index.ReindexOptions{Threshold: &lowThreshold})
	require.NoError(t, err)
	require.NoError(t, storeLow.Load("", "test-model", 2, ""))
	require.NoError(t, storeLow.BuildIndex(ctx, "test-model", 3, provider, lowLocalProvider))

	modifyADRFile(t, lowTmpDir, 0)

	outputLow := captureStdout(t, func() {
		err = storeLow.BuildIndex(ctx, "test-model", 3, provider, lowLocalProvider)
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

	storeDefault, err := index.NewPgStore(connStr, "reindex_concurrently_default", 5, index.ReindexOptions{})
	require.NoError(t, err)
	require.NoError(t, storeDefault.Load("", "test-model", 2, ""))

	outputDefault := captureStdout(t, func() {
		err = storeDefault.BuildIndex(ctx, "test-model", 3, provider, defaultLocalProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, outputDefault, "Rebuilding HNSW index (concurrently)", "default should use the non-blocking CONCURRENTLY form")
	assert.NotContains(t, outputDefault, "Warning: failed to reindex", "REINDEX INDEX CONCURRENTLY should actually succeed against a real pgvector HNSW index")

	// Explicit false: should log the "(blocking)" mode instead.
	blocking := false
	blockingTmpDir := t.TempDir()
	writeADRFiles(t, blockingTmpDir, 3)
	blockingLocalProvider := index.NewLocalProvider(blockingTmpDir, []string{"Accepted"})

	storeBlocking, err := index.NewPgStore(connStr, "reindex_concurrently_blocking", 5, index.ReindexOptions{Concurrently: &blocking})
	require.NoError(t, err)
	require.NoError(t, storeBlocking.Load("", "test-model", 2, ""))

	outputBlocking := captureStdout(t, func() {
		err = storeBlocking.BuildIndex(ctx, "test-model", 3, provider, blockingLocalProvider)
	})
	require.NoError(t, err)
	assert.Contains(t, outputBlocking, "Rebuilding HNSW index (blocking)", "explicit false should use the blocking REINDEX form")
	assert.NotContains(t, outputBlocking, "(concurrently)")
	assert.NotContains(t, outputBlocking, "Warning: failed to reindex", "the blocking REINDEX INDEX form should actually succeed too")
}
