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

// HNSWOptions controls PgStore's HNSW tuning. *bool fields default to
// true when nil -- a plain bool can't distinguish "unset" from "false".
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

// IterativeScanSupportedVersion reports whether version is >= 0.8.0, which
// introduced the hnsw.iterative_scan GUC.
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

// PgvectorVersionQuery is exported so pgvector_bench_test.go's version
// probe stays in sync with NewPgStore's, instead of a copy that could drift.
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

	// A query error here is treated as unsupported, not a fatal error.
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

// ensureSchema creates the archguard_adrs table and HNSW index if they don't
// exist, and adds the adr_id/scope columns if missing. Both Load and
// BuildIndex call this -- BuildIndex must be self-sufficient since
// cli.runIndex calls it without a preceding Load.
func (s *PgStore) ensureSchema(ctx context.Context, dim int) error {
	createQuery := fmt.Sprintf(`
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
	if _, err := s.pool.Exec(ctx, createQuery); err != nil {
		return err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'archguard_adrs' AND table_schema = current_schema() AND column_name IN ('adr_id', 'scope')
	`)
	if err != nil {
		return err
	}
	present := make(map[string]bool)
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			rows.Close()
			return err
		}
		present[col] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var alters []string
	if !present["adr_id"] {
		alters = append(alters, "ADD COLUMN IF NOT EXISTS adr_id TEXT")
	}
	if !present["scope"] {
		alters = append(alters, "ADD COLUMN IF NOT EXISTS scope TEXT")
	}
	if len(alters) > 0 {
		_, err := s.pool.Exec(ctx, "ALTER TABLE archguard_adrs "+strings.Join(alters, ", "))
		return err
	}
	return nil
}

// Load verifies the database connection and ensures the tables exist.
func (s *PgStore) Load(path, modelName string, dim int, currentHash string) error {
	return s.ensureSchema(context.Background(), dim)
}

// Save is a no-op for PgStore as data is persisted immediately during BuildIndex.
func (s *PgStore) Save(path string) error {
	return nil
}

// BuildIndex parses the ADRs, generates embeddings, and inserts them into the database.
func (s *PgStore) BuildIndex(ctx context.Context, modelName string, dim int, provider llm.Provider, adrProvider Provider) error {
	if err := s.ensureSchema(ctx, dim); err != nil {
		return fmt.Errorf("failed to ensure schema: %w", err)
	}

	validADRs, err := adrProvider.GetADRs(ctx)
	if err != nil {
		return err
	}

	// Fetch existing ADRs from database for this project
	rows, err := s.pool.Query(ctx, "SELECT rel_path, title, status, content, COALESCE(adr_id, ''), COALESCE(scope, '') FROM archguard_adrs WHERE project_name = $1", s.projectName)
	if err != nil {
		return fmt.Errorf("failed to query existing ADRs: %w", err)
	}
	defer rows.Close()

	existingMap := make(map[string]ADR)
	for rows.Next() {
		var relPath, title, status, content, adrID, scope string
		if err := rows.Scan(&relPath, &title, &status, &content, &adrID, &scope); err != nil {
			return fmt.Errorf("failed to scan existing ADR row: %w", err)
		}
		existingMap[relPath] = ADR{
			ID:      adrID,
			Title:   title,
			Status:  status,
			Content: content,
			Scope:   scope,
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read existing ADRs: %w", err)
	}

	var adrsToEmbed []int
	var adrsToSync []int
	for i, valid := range validADRs {
		existing, ok := existingMap[valid.RelPath]
		switch {
		case !ok || existing.Content != valid.Content || existing.Title != valid.Title || existing.Status != valid.Status:
			adrsToEmbed = append(adrsToEmbed, i)
		case existing.ID != valid.ID || existing.Scope != valid.Scope:
			adrsToSync = append(adrsToSync, i)
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
					INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding, adr_id, scope)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
					ON CONFLICT (project_name, rel_path) DO UPDATE SET
						title = EXCLUDED.title,
						status = EXCLUDED.status,
						content = EXCLUDED.content,
						embedding = EXCLUDED.embedding,
						adr_id = EXCLUDED.adr_id,
						scope = EXCLUDED.scope
				`, s.projectName, validADRs[idx].RelPath, validADRs[idx].Title, validADRs[idx].Status, validADRs[idx].Content, vec, validADRs[idx].ID, validADRs[idx].Scope)
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

	if len(adrsToSync) > 0 {
		fmt.Printf("Syncing ID/scope metadata for %d unchanged ADR(s)...\n", len(adrsToSync))
		batch := &pgx.Batch{}
		for _, idx := range adrsToSync {
			batch.Queue(`
				UPDATE archguard_adrs SET adr_id = $1, scope = $2
				WHERE project_name = $3 AND rel_path = $4
			`, validADRs[idx].ID, validADRs[idx].Scope, s.projectName, validADRs[idx].RelPath)
		}

		br := s.pool.SendBatch(ctx, batch)
		for _, idx := range adrsToSync {
			tag, err := br.Exec()
			if err != nil {
				_ = br.Close()
				return fmt.Errorf("failed to sync metadata for ADR %s: %w", validADRs[idx].RelPath, err)
			}
			if tag.RowsAffected() == 0 {
				fmt.Printf("Warning: sync UPDATE for %s affected 0 rows (row may have been deleted concurrently)\n", validADRs[idx].RelPath)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("failed to close sync batch: %w", err)
		}
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

// SearchQuery is exported so pgvector_bench_test.go can EXPLAIN this exact
// query, instead of a copy that could drift.
const SearchQuery = `
	SELECT rel_path, title, status, content, COALESCE(adr_id, '') AS adr_id, COALESCE(scope, '') AS scope, (1 - (embedding <=> $1)) as similarity
	FROM archguard_adrs
	WHERE project_name = $2 AND embedding <=> $1 <= $3
	ORDER BY embedding <=> $1
	LIMIT $4
`

// maxSearchCandidates bounds PgStore.Search's fetch so Go-side scope
// filtering (SQL can't evaluate a doublestar glob) sees every candidate.
const maxSearchCandidates = 1000

// Search returns up to topK ADRs above threshold cosine similarity whose
// scope (if any) matches filePath, scope-filtered before the topK cut.
func (s *PgStore) Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	// pgvector uses <=> for cosine distance. Similarity is 1 - distance.
	// So similarity >= threshold means distance <= 1 - threshold.
	distanceThreshold := 1.0 - threshold

	rows, err := s.pool.Query(ctx, SearchQuery, vec, s.projectName, distanceThreshold, maxSearchCandidates)
	if err != nil {
		fmt.Printf("PgStore Search query failed: %v\n", err)
		return nil
	}
	defer rows.Close()

	var candidates []SearchResult
	for rows.Next() {
		var adr ADR
		var score float64
		if err := rows.Scan(&adr.RelPath, &adr.Title, &adr.Status, &adr.Content, &adr.ID, &adr.Scope, &score); err != nil {
			fmt.Printf("PgStore Row scan failed: %v\n", err)
			continue
		}

		candidates = append(candidates, SearchResult{
			ADR:   &adr,
			Score: score,
		})
	}

	candidates = filterByScope(candidates, filePath)
	return rankAndLimit(candidates, topK)
}
