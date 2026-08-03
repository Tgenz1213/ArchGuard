package index_test

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tgenz1213/archguard/internal/index"
)

func TestRandomVector_ReturnsRequestedDimension(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	v := randomVector(rng, 1536)
	assert.Len(t, v, 1536)
	for _, x := range v {
		assert.GreaterOrEqual(t, x, float32(-1))
		assert.LessOrEqual(t, x, float32(1))
	}
}

func TestComputeRecall(t *testing.T) {
	tests := []struct {
		name        string
		hnsw        []string
		groundTruth []string
		want        float64
	}{
		{"perfect match", []string{"a.md", "b.md", "c.md"}, []string{"a.md", "b.md", "c.md"}, 1.0},
		{"partial match", []string{"a.md", "x.md"}, []string{"a.md", "b.md"}, 0.5},
		{"no match", []string{"x.md", "y.md"}, []string{"a.md", "b.md"}, 0.0},
		{"empty ground truth is vacuously full recall", []string{"a.md"}, []string{}, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeRecall(tt.hnsw, tt.groundTruth))
		})
	}
}

func TestLatencyPercentiles(t *testing.T) {
	latencies := []time.Duration{
		100 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		200 * time.Millisecond,
		20 * time.Millisecond,
	}
	// sorted: 10, 20, 50, 100, 200 (ms) -- len=5, p50 idx=2 (50ms), p95 idx=4 (200ms)
	p50, p95 := latencyPercentiles(latencies)
	assert.Equal(t, 50*time.Millisecond, p50)
	assert.Equal(t, 200*time.Millisecond, p95)
}

func TestLatencyPercentiles_Empty(t *testing.T) {
	p50, p95 := latencyPercentiles(nil)
	assert.Equal(t, time.Duration(0), p50)
	assert.Equal(t, time.Duration(0), p95)
}

func TestIterativeScanSupportedVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"pre-0.8", "0.7.0", false},
		{"0.8.0 supported", "0.8.0", true},
		{"0.8.5 supported", "0.8.5", true},
		{"1.0.0 supported", "1.0.0", true},
		{"malformed string", "abc", false},
		{"single component", "0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, iterativeScanSupportedVersion(tt.version))
		})
	}
}

func TestGroundTruthSearch_ForcesSeqScanAndMatchesExactOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "gt_test_project", 5, index.ReindexOptions{})
	require.NoError(t, err)
	require.NoError(t, store.Load("", "test-model", 2, ""))

	pool := newBenchAdminPool(t, ctx, connStr)

	vectors := map[string][]float32{
		"same.md":       {1, 0},
		"orthogonal.md": {0, 1},
		"diag.md":       {1, 1},
		"opposite.md":   {-1, 0},
		"near.md":       {0.9, 0.1},
	}
	for relPath, v := range vectors {
		_, err := pool.Exec(ctx, `
			INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, "gt_test_project", relPath, relPath, "Accepted", "content", pgvector.NewVector(v))
		require.NoError(t, err)
	}

	// Hand-computed cosine distance from query (1,0), ascending:
	// same.md=0, near.md=0.0061, diag.md=0.2929, orthogonal.md=1, opposite.md=2
	got, err := groundTruthSearch(ctx, pool, []float32{1, 0}, "gt_test_project", -1.0, 5)
	require.NoError(t, err)
	assert.Equal(t, []string{"same.md", "near.md", "diag.md", "orthogonal.md", "opposite.md"}, got)

	// Confirm the seqscan actually avoided the HNSW index, or this isn't independent ground truth.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SET LOCAL enable_indexscan = off")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SET LOCAL enable_bitmapscan = off")
	require.NoError(t, err)

	rows, err := tx.Query(ctx, `
		EXPLAIN SELECT rel_path FROM archguard_adrs
		WHERE project_name = $2 AND embedding <=> $1 <= $3
		ORDER BY embedding <=> $1 LIMIT $4
	`, pgvector.NewVector([]float32{1, 0}), "gt_test_project", 2.0, 5)
	require.NoError(t, err)
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan.WriteString(line)
	}
	require.NoError(t, rows.Err())
	assert.NotContains(t, plan.String(), "Index Scan", "expected seqscan-forced ground truth query to avoid the HNSW index")
}

// newBenchAdminPool is for operations PgStore's public API doesn't expose: seeding, TRUNCATE, ground truth, GUC changes.
func newBenchAdminPool(tb testing.TB, ctx context.Context, connStr string) *pgxpool.Pool {
	tb.Helper()

	config, err := pgxpool.ParseConfig(connStr)
	require.NoError(tb, err)
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	return pool
}

// groundTruthSearch mirrors PgStore.Search's query with the HNSW index disabled, for the exact nearest-neighbor order.
func groundTruthSearch(ctx context.Context, pool *pgxpool.Pool, queryEmbedding []float32, projectName string, threshold float64, topK int) ([]string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL enable_indexscan = off"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL enable_bitmapscan = off"); err != nil {
		return nil, err
	}

	vec := pgvector.NewVector(queryEmbedding)
	distanceThreshold := 1.0 - threshold

	rows, err := tx.Query(ctx, `
		SELECT rel_path
		FROM archguard_adrs
		WHERE project_name = $2 AND embedding <=> $1 <= $3
		ORDER BY embedding <=> $1
		LIMIT $4
	`, vec, projectName, distanceThreshold, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relPaths []string
	for rows.Next() {
		var relPath string
		if err := rows.Scan(&relPath); err != nil {
			return nil, err
		}
		relPaths = append(relPaths, relPath)
	}
	return relPaths, rows.Err()
}

func randomVector(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(rng.Float64()*2 - 1)
	}
	return v
}

// computeRecall is the fraction of groundTruthRelPaths also in hnswRelPaths; empty ground truth is vacuous full recall.
func computeRecall(hnswRelPaths, groundTruthRelPaths []string) float64 {
	if len(groundTruthRelPaths) == 0 {
		return 1.0
	}

	inGroundTruth := make(map[string]bool, len(groundTruthRelPaths))
	for _, rp := range groundTruthRelPaths {
		inGroundTruth[rp] = true
	}

	matched := 0
	for _, rp := range hnswRelPaths {
		if inGroundTruth[rp] {
			matched++
		}
	}
	return float64(matched) / float64(len(groundTruthRelPaths))
}

func latencyPercentiles(latencies []time.Duration) (p50, p95 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	p50 = sorted[len(sorted)*50/100]
	idx95 := len(sorted) * 95 / 100
	if idx95 >= len(sorted) {
		idx95 = len(sorted) - 1
	}
	p95 = sorted[idx95]
	return p50, p95
}

func TestSeedProjectADRs_InsertsExpectedRowCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "seed_test_project", 5, index.ReindexOptions{})
	require.NoError(t, err)
	require.NoError(t, store.Load("", "test-model", 8, ""))

	pool := newBenchAdminPool(t, ctx, connStr)
	rng := rand.New(rand.NewSource(1))

	require.NoError(t, seedProjectADRs(ctx, pool, rng, "seed_test_project", 30, 8))

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM archguard_adrs WHERE project_name = $1", "seed_test_project").Scan(&count))
	assert.Equal(t, 30, count)
}

func TestProbeIterativeScanSupport_ReturnsVersionWithoutError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := setupPgContainer(t, ctx)

	store, err := index.NewPgStore(connStr, "probe_test_project", 5, index.ReindexOptions{})
	require.NoError(t, err)
	require.NoError(t, store.Load("", "test-model", 2, ""))

	pool := newBenchAdminPool(t, ctx, connStr)

	available, version, err := probeIterativeScanSupport(ctx, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, version, "expected a pgvector extension version string")
	t.Logf("pgvector extension version %s: iterative scan support = %v", version, available)
}

// seedProjectADRs inserts synthetic ADRs directly via SQL, sequentially (not BuildIndex's concurrent pattern) so insertion order — and thus the resulting HNSW graph — is reproducible across runs.
func seedProjectADRs(ctx context.Context, pool *pgxpool.Pool, rng *rand.Rand, projectName string, count int, dim int) error {
	for i := 0; i < count; i++ {
		relPath := fmt.Sprintf("adr_%d.md", i)
		vec := pgvector.NewVector(randomVector(rng, dim))
		if _, err := pool.Exec(ctx, `
			INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, projectName, relPath, relPath, "Accepted", "synthetic benchmark content", vec); err != nil {
			return err
		}
	}
	return nil
}

// probeIterativeScanSupport reports the pgvector version and whether hnsw.iterative_scan (0.8.0+) is supported.
func probeIterativeScanSupport(ctx context.Context, pool *pgxpool.Pool) (available bool, pgvectorVersion string, err error) {
	if err := pool.QueryRow(ctx, "SELECT extversion FROM pg_extension WHERE extname = 'vector'").Scan(&pgvectorVersion); err != nil {
		return false, "", fmt.Errorf("failed to read pgvector extension version: %w", err)
	}
	return iterativeScanSupportedVersion(pgvectorVersion), pgvectorVersion, nil
}

// iterativeScanSupportedVersion reports whether version is pgvector 0.8.0 or later.
func iterativeScanSupportedVersion(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major > 0 || minor >= 8
}
