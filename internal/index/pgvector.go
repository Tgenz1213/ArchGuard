package index

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/tgenz1213/archguard/internal/llm"
	"golang.org/x/sync/errgroup"
)

const defaultReindexThreshold = 0.20

const hnswIndexName = "archguard_adrs_embedding_idx"

// HNSWOptions controls PgStore's HNSW-related tuning: automatic index
// maintenance and iterative-scan behavior. Enabled, Concurrently, and
// IterativeScan are *bool, not bool, because they default to true when
// unset -- a plain bool's zero value can't represent "unset" vs "explicitly false".
type HNSWOptions struct {
	Enabled       *bool    // nil = enabled
	Threshold     *float64 // nil = defaultReindexThreshold; explicit 0.0 reindexes on any churn
	Concurrently  *bool    // nil = REINDEX INDEX CONCURRENTLY; explicit false = blocking REINDEX INDEX
	IterativeScan *bool    // nil = enabled when pgvector supports it; explicit false disables
}

// PgStore implements the VectorStore interface using PostgreSQL and pgvector.
type PgStore struct {
	pool             *pgxpool.Pool
	connectionString string
	projectName      string
	concurrency      int
	hnsw             HNSWOptions
}

// IterativeScanSupportedVersion reports whether version (a pgvector
// extension version string, e.g. "0.8.0") is 0.8.0 or later, the version
// that introduced the hnsw.iterative_scan GUC.
func IterativeScanSupportedVersion(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 1 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	if errMajor != nil {
		return false
	}
	if major > 0 {
		return true
	}
	if len(parts) < 2 {
		return false
	}
	minor, errMinor := strconv.Atoi(parts[1])
	if errMinor != nil {
		return false
	}
	return minor >= 8
}

// PgvectorVersionQuery reads the installed pgvector extension's version string,
// exported so the benchmark's version probe (internal/index/pgvector_bench_test.go)
// can run the exact same query NewPgStore does, rather than a copy that could drift.
const PgvectorVersionQuery = "SELECT extversion FROM pg_extension WHERE extname = 'vector'"

// NewPgStore initializes a new PgStore connected to the given database URL.
// hnsw controls automatic HNSW index maintenance and iterative-scan behavior.
func NewPgStore(connStr string, projectName string, concurrency int, hnsw HNSWOptions) (*PgStore, error) {
	ctx := context.Background()

	// Ensure the vector extension exists BEFORE setting up the pool
	tempConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to initially connect to database: %w", err)
	}
	_, err = tempConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		_ = tempConn.Close(ctx)
		return nil, fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Probe the installed pgvector version to decide whether
	// hnsw.iterative_scan (0.8.0+) can safely be applied. A query error here
	// is treated as unsupported (fail-safe) rather than failing NewPgStore.
	var pgvectorVersion string
	versionErr := tempConn.QueryRow(ctx, PgvectorVersionQuery).Scan(&pgvectorVersion)
	_ = tempConn.Close(ctx)

	iterativeScanWanted := hnsw.iterativeScanConfigured()
	applyIterativeScan := false
	switch {
	case versionErr != nil:
		if iterativeScanWanted {
			fmt.Printf("Warning: failed to check pgvector version for hnsw.iterative_scan support (%v); leaving it disabled for all connections from this store.\n", versionErr)
		}
	case iterativeScanWanted && IterativeScanSupportedVersion(pgvectorVersion):
		applyIterativeScan = true
	case iterativeScanWanted:
		fmt.Printf("Warning: pgvector %s does not support hnsw.iterative_scan (requires 0.8.0+); project-filtered search recall may be degraded at scale. See docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md.\n", pgvectorVersion)
	}

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
			return err
		}
		if applyIterativeScan {
			if _, err := conn.Exec(ctx, "SET hnsw.iterative_scan = 'relaxed_order'"); err != nil {
				fmt.Printf("Warning: failed to enable hnsw.iterative_scan on a new connection (%v); this connection will use standard (non-iterative) HNSW search instead.\n", err)
			}
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &PgStore{
		pool:             pool,
		connectionString: connStr,
		projectName:      projectName,
		concurrency:      concurrency,
		hnsw:             hnsw,
	}, nil
}

// Pool exposes the store's connection pool for integration tests that need to
// inspect real pooled-connection state (e.g. AfterConnect-applied GUCs).
func (s *PgStore) Pool() *pgxpool.Pool { return s.pool }

// Close releases the store's connection pool.
func (s *PgStore) Close() {
	s.pool.Close()
}

func (s *PgStore) reindexEnabled() bool {
	if s.hnsw.Enabled == nil {
		return true
	}
	return *s.hnsw.Enabled
}

func (s *PgStore) reindexThreshold() float64 {
	if s.hnsw.Threshold == nil {
		return defaultReindexThreshold
	}
	return *s.hnsw.Threshold
}

func (s *PgStore) reindexConcurrently() bool {
	if s.hnsw.Concurrently == nil {
		return true
	}
	return *s.hnsw.Concurrently
}

func (o HNSWOptions) iterativeScanConfigured() bool {
	if o.IterativeScan == nil {
		return true
	}
	return *o.IterativeScan
}

func (s *PgStore) reindexStatement() string {
	if s.reindexConcurrently() {
		return "REINDEX INDEX CONCURRENTLY " + hnswIndexName
	}
	return "REINDEX INDEX " + hnswIndexName
}

// CalculateHash is a no-op for PgStore because the database maintains state incrementally (or is completely truncated on Build).
func (s *PgStore) CalculateHash(adrs []ADR, modelName string) (string, error) {
	return "remote", nil
}

// Load verifies the database connection and ensures the tables exist.
func (s *PgStore) Load(path, modelName string, dim int, currentHash string) error {
	ctx := context.Background()

	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS archguard_adrs (
			id SERIAL PRIMARY KEY,
			project_name TEXT NOT NULL DEFAULT 'default',
			rel_path TEXT,
			title TEXT,
			status TEXT,
			content TEXT,
			embedding vector(%d),
			UNIQUE (project_name, rel_path)
		);
		CREATE INDEX IF NOT EXISTS %s ON archguard_adrs USING hnsw (embedding vector_cosine_ops);
	`, dim, hnswIndexName)

	_, err := s.pool.Exec(ctx, query)
	return err
}

// Save is a no-op for PgStore as data is persisted immediately during BuildIndex.
func (s *PgStore) Save(path string) error {
	return nil
}

// BuildIndex parses the ADRs, generates embeddings, and inserts them into the database.
func (s *PgStore) BuildIndex(ctx context.Context, modelName string, dim int, provider llm.Provider, adrProvider Provider) error {
	validADRs, err := adrProvider.GetADRs(ctx)
	if err != nil {
		return err
	}

	// Fetch existing ADRs from database for this project
	rows, err := s.pool.Query(ctx, "SELECT rel_path, title, status, content FROM archguard_adrs WHERE project_name = $1", s.projectName)
	if err != nil {
		return fmt.Errorf("failed to query existing ADRs: %w", err)
	}
	defer rows.Close()

	existingMap := make(map[string]ADR)
	for rows.Next() {
		var relPath, title, status, content string
		if err := rows.Scan(&relPath, &title, &status, &content); err != nil {
			continue
		}
		existingMap[relPath] = ADR{
			Title:   title,
			Status:  status,
			Content: content,
		}
	}

	var adrsToEmbed []int
	for i, valid := range validADRs {
		existing, ok := existingMap[valid.RelPath]
		if ok && existing.Content == valid.Content && existing.Title == valid.Title && existing.Status == valid.Status {
			// Already embedded and unchanged
		} else {
			adrsToEmbed = append(adrsToEmbed, i)
		}
	}

	fmt.Printf("Found %d valid ADRs. Generating embeddings for %d new/modified ADRs...\n", len(validADRs), len(adrsToEmbed))

	if len(adrsToEmbed) > 0 {
		concurrency := s.concurrency
		if concurrency <= 0 {
			concurrency = 5
		}

		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)

		for _, idx := range adrsToEmbed {
			idx := idx
			g.Go(func() error {
				textToEmbed := fmt.Sprintf("Title: %s\nStatus: %s\nContent: %s", validADRs[idx].Title, validADRs[idx].Status, validADRs[idx].Content)
				emb, err := provider.CreateEmbedding(gCtx, textToEmbed, llm.EmbeddingTaskDocument)
				if err != nil {
					return fmt.Errorf("failed to embed ADR %s: %w", validADRs[idx].RelPath, err)
				}
				validADRs[idx].Embedding = emb

				vec := pgvector.NewVector(emb)
				_, err = s.pool.Exec(gCtx, `
					INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
					VALUES ($1, $2, $3, $4, $5, $6)
					ON CONFLICT (project_name, rel_path) DO UPDATE SET
						title = EXCLUDED.title,
						status = EXCLUDED.status,
						content = EXCLUDED.content,
						embedding = EXCLUDED.embedding
				`, s.projectName, validADRs[idx].RelPath, validADRs[idx].Title, validADRs[idx].Status, validADRs[idx].Content, vec)
				if err != nil {
					return fmt.Errorf("failed to upsert ADR %s: %w", validADRs[idx].RelPath, err)
				}
				fmt.Printf(".")
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return err
		}
		fmt.Println()
	}

	// Delete missing ADRs
	validMap := make(map[string]bool)
	for _, valid := range validADRs {
		validMap[valid.RelPath] = true
	}

	var toDelete []string
	for relPath := range existingMap {
		if !validMap[relPath] {
			toDelete = append(toDelete, relPath)
		}
	}

	if len(toDelete) > 0 {
		fmt.Printf("Deleting %d removed ADRs from database...\n", len(toDelete))
		for _, relPath := range toDelete {
			_, err := s.pool.Exec(ctx, "DELETE FROM archguard_adrs WHERE project_name = $1 AND rel_path = $2", s.projectName, relPath)
			if err != nil {
				return fmt.Errorf("failed to delete ADR %s: %w", relPath, err)
			}
		}
	}

	// Conditional HNSW maintenance routine
	if s.reindexEnabled() {
		modifiedCount := len(adrsToEmbed) + len(toDelete)
		totalCount := len(validADRs) + len(toDelete)
		threshold := s.reindexThreshold()
		if totalCount > 0 && float64(modifiedCount)/float64(totalCount) >= threshold {
			mode := "blocking"
			if s.reindexConcurrently() {
				mode = "concurrently"
			}
			fmt.Printf("Modifications exceeded %.0f%% threshold. Rebuilding HNSW index (%s)...\n", threshold*100, mode)
			if _, err := s.pool.Exec(ctx, s.reindexStatement()); err != nil {
				fmt.Printf("Warning: failed to reindex HNSW graph: %v\n", err)
			}
		}
	}

	return nil
}

// SearchQuery is PgStore.Search's query text, exported so the benchmark's HNSW-usage
// guard (internal/index/pgvector_bench_test.go) can EXPLAIN the exact same query
// Search runs, rather than a copy that could silently drift out of sync.
const SearchQuery = `
	SELECT rel_path, title, status, content, (1 - (embedding <=> $1)) as similarity
	FROM archguard_adrs
	WHERE project_name = $2 AND embedding <=> $1 <= $3
	ORDER BY embedding <=> $1
	LIMIT $4
`

// Search performs a vector similarity search across the Postgres store using cosine distance.
func (s *PgStore) Search(queryEmbedding []float32, threshold float64, topK int) []SearchResult {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	// pgvector uses <=> for cosine distance. Similarity is 1 - distance.
	// So similarity >= threshold means distance <= 1 - threshold.
	distanceThreshold := 1.0 - threshold

	rows, err := s.pool.Query(ctx, SearchQuery, vec, s.projectName, distanceThreshold, topK)
	if err != nil {
		fmt.Printf("PgStore Search query failed: %v\n", err)
		return nil
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var adr ADR
		var score float64
		if err := rows.Scan(&adr.RelPath, &adr.Title, &adr.Status, &adr.Content, &score); err != nil {
			fmt.Printf("PgStore Row scan failed: %v\n", err)
			continue
		}

		results = append(results, SearchResult{
			ADR:   &adr,
			Score: score,
		})
	}

	return results
}
