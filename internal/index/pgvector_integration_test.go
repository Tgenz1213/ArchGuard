package index_test

import (
	"context"
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

func TestPgStore_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// 1. Spin up pgvector container
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
			t.Skipf("Skipping integration test: Docker is not available on this host (%v)", err)
		}
		require.NoError(t, err)
	}

	// Clean up the container
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
	}()

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// 2. Initialize PgStore
	store, err := index.NewPgStore(connStr, "integration_test_project", 5)
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
	provider := &llm.MockProvider{
		EmbeddingDim: 2,
		EmbedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return []float32{0.1, 0.1}, nil
		},
	}
	localProvider := index.NewLocalProvider(tmpDir, []string{"Accepted"})
	err = store.BuildIndex(ctx, "test-model", 3, provider, localProvider)
	require.NoError(t, err)

	// Insert into a second project to test isolation
	storeOther, err := index.NewPgStore(connStr, "other_project", 5)
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
