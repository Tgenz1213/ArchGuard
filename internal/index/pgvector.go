package index

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

const defaultReindexThreshold = 0.20

const hnswIndexName = "archguard_adrs_embedding_idx"

// A nil *bool means enabled; a plain bool can't tell unset from false.
type HNSWOptions struct {
	Enabled       *bool    // nil = enabled
	Threshold     *float64 // nil = defaultReindexThreshold; explicit 0.0 reindexes on any churn
	Concurrently  *bool    // nil = REINDEX INDEX CONCURRENTLY; explicit false = blocking REINDEX INDEX
	IterativeScan *bool    // nil = enabled when pgvector supports it; explicit false disables
}

type PgStore struct {
	pool             *pgxpool.Pool
	connectionString string
	projectName      string
	concurrency      int
	hnsw             HNSWOptions
	out              *output.Printer
	adrsMu           sync.Mutex
	adrs             []ADR
	adrsLoaded       bool
}

// hnsw.iterative_scan exists from pgvector 0.8.0.
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

func NewPgStore(connStr string, projectName string, concurrency int, hnsw HNSWOptions, out *output.Printer) (*PgStore, error) {
	ctx := context.Background()

	probe, err := ensureVectorExtension(ctx, connStr)
	if err != nil {
		return nil, err
	}

	applyIterativeScan := iterativeScanDecision(probe, hnsw.iterativeScanConfigured(), out)

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	config.AfterConnect = afterConnect(applyIterativeScan, out)

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
		out:              out,
	}, nil
}

type extensionProbe struct {
	version    string
	versionErr error
}

func ensureVectorExtension(ctx context.Context, connStr string) (extensionProbe, error) {
	// The extension must exist before the pool's AfterConnect registers vector types.
	tempConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return extensionProbe{}, fmt.Errorf("failed to initially connect to database: %w", err)
	}

	_, err = tempConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		_ = tempConn.Close(ctx) //nolint:errcheck // cleanup; the extension error wins

		return extensionProbe{}, fmt.Errorf("failed to create vector extension: %w", err)
	}

	// A query error here is treated as unsupported, not a fatal error.
	var probe extensionProbe

	probe.versionErr = tempConn.QueryRow(ctx, PgvectorVersionQuery).Scan(&probe.version)
	_ = tempConn.Close(ctx) //nolint:errcheck // a one-off probe connection; the pool opens its own

	return probe, nil
}

func iterativeScanDecision(probe extensionProbe, wanted bool, out *output.Printer) bool {
	switch {
	case probe.versionErr != nil:
		if wanted {
			out.Warn("failed to check pgvector version for hnsw.iterative_scan support (%v); leaving it disabled for all connections from this store.", probe.versionErr)
		}
	case wanted && IterativeScanSupportedVersion(probe.version):
		return true
	case wanted:
		out.Warn("pgvector %s does not support hnsw.iterative_scan (requires 0.8.0+); project-filtered search recall may be degraded at scale. See docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md.", probe.version)
	}

	return false
}

func afterConnect(applyIterativeScan bool, out *output.Printer) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
			return err
		}

		if applyIterativeScan {
			if _, err := conn.Exec(ctx, "SET hnsw.iterative_scan = 'relaxed_order'"); err != nil {
				out.Warn("failed to enable hnsw.iterative_scan on a new connection (%v); this connection will use standard (non-iterative) HNSW search instead.", err)
			}
		}

		return nil
	}
}

func (s *PgStore) Pool() *pgxpool.Pool { return s.pool }

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

// Constant: the DB keeps its own state, so a hash mismatch never triggers a rebuild.
func (s *PgStore) CalculateHash(adrs []ADR, modelName string) (string, error) {
	return "remote", nil
}

// Both Load and BuildIndex call this: cli.runIndex calls BuildIndex without a preceding Load.
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
		WHERE table_name = 'archguard_adrs' AND table_schema = current_schema() AND column_name IN ('adr_id', 'scope', 'similarity_threshold', 'rules')
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

	if !present["similarity_threshold"] {
		alters = append(alters, "ADD COLUMN IF NOT EXISTS similarity_threshold DOUBLE PRECISION")
	}

	if !present["rules"] {
		alters = append(alters, "ADD COLUMN IF NOT EXISTS rules TEXT")
	}

	if len(alters) > 0 {
		_, err := s.pool.Exec(ctx, "ALTER TABLE archguard_adrs "+strings.Join(alters, ", "))
		return err
	}

	return nil
}

func (s *PgStore) Load(path, modelName string, dim int, currentHash string) error {
	return s.ensureSchema(context.Background(), dim)
}

// No-op: BuildIndex persists immediately.
func (s *PgStore) Save(path string) error {
	return nil
}

func thresholdsEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func rulesEqual(a, b Rules) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}

	return reflect.DeepEqual(a, b)
}

func (s *PgStore) BuildIndex(ctx context.Context, modelName string, dim int, embedder inference.Embedder, adrProvider Provider) (BuildIndexResult, error) {
	defer s.dropADRCache()

	if err := s.ensureSchema(ctx, dim); err != nil {
		return BuildIndexResult{}, fmt.Errorf("failed to ensure schema: %w", err)
	}

	validADRs, stats, err := adrProvider.GetADRs(ctx)
	if err != nil {
		return BuildIndexResult{}, err
	}

	existing, err := s.loadExistingADRs(ctx)
	if err != nil {
		return BuildIndexResult{}, err
	}

	adrsToEmbed, adrsToSync := classifyADRs(existing, validADRs)

	result := BuildIndexResult{Summary: summarizeCorpus(validADRs, stats), Attempted: true}
	job := embedJob{adrs: validADRs, embedder: embedder, concurrency: s.concurrency, out: s.out, embedLabel: "embed", persist: s.upsertADR}

	outcome, err := job.embedInto(ctx, adrsToEmbed, &result)
	if err != nil {
		return result, err
	}

	if err := s.syncMetadata(ctx, validADRs, adrsToSync); err != nil {
		return result, err
	}

	removed, err := s.deleteRemoved(ctx, existing, validADRs)
	if err != nil {
		return result, err
	}

	s.reindexIfChurned(ctx, len(adrsToEmbed)-len(outcome.failed)+len(removed), len(validADRs)+len(removed))

	return result, nil
}

func (s *PgStore) loadExistingADRs(ctx context.Context) (map[string]ADR, error) {
	rows, err := s.pool.Query(ctx, "SELECT rel_path, title, status, content, COALESCE(adr_id, ''), COALESCE(scope, ''), similarity_threshold, rules FROM archguard_adrs WHERE project_name = $1", s.projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing ADRs: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]ADR)

	for rows.Next() {
		var relPath, title, status, content, adrID, scope string

		var similarityThreshold *float64

		var rules Rules
		if err := rows.Scan(&relPath, &title, &status, &content, &adrID, &scope, &similarityThreshold, &rules); err != nil {
			return nil, fmt.Errorf("failed to scan existing ADR row: %w", err)
		}

		existing[relPath] = ADR{
			ID:                  adrID,
			Title:               title,
			Status:              status,
			Content:             content,
			Scope:               ParseScopePatterns(scope),
			SimilarityThreshold: similarityThreshold,
			Rules:               rules,
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read existing ADRs: %w", err)
	}

	return existing, nil
}

func classifyADRs(existing map[string]ADR, adrs []ADR) (toEmbed, toSync []int) {
	for i, adr := range adrs {
		stored, ok := existing[adr.RelPath]

		switch {
		case !ok || !adrUnchanged(stored, adr):
			toEmbed = append(toEmbed, i)
		case stored.ID != adr.ID || !slices.Equal(stored.Scope, adr.Scope) || !thresholdsEqual(stored.SimilarityThreshold, adr.SimilarityThreshold) ||
			!rulesEqual(stored.Rules, adr.Rules):
			toSync = append(toSync, i)
		}
	}

	return toEmbed, toSync
}

func (s *PgStore) upsertADR(ctx context.Context, adr ADR) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding, adr_id, scope, similarity_threshold, rules)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (project_name, rel_path) DO UPDATE SET
			title = EXCLUDED.title,
			status = EXCLUDED.status,
			content = EXCLUDED.content,
			embedding = EXCLUDED.embedding,
			adr_id = EXCLUDED.adr_id,
			scope = EXCLUDED.scope,
			similarity_threshold = EXCLUDED.similarity_threshold,
			rules = EXCLUDED.rules
	`, s.projectName, adr.RelPath, adr.Title, adr.Status, adr.Content, pgvector.NewVector(adr.Embedding), adr.ID, adr.Scope, adr.SimilarityThreshold, adr.Rules)
	if err != nil {
		return fmt.Errorf("upsert: %w", err)
	}

	return nil
}

func (s *PgStore) syncMetadata(ctx context.Context, adrs []ADR, toSync []int) error {
	if len(toSync) == 0 {
		return nil
	}

	s.out.Info("Syncing ID/scope/threshold metadata for %d unchanged ADR(s)...", len(toSync))

	batch := &pgx.Batch{}
	for _, idx := range toSync {
		batch.Queue(`
			UPDATE archguard_adrs SET adr_id = $1, scope = $2, similarity_threshold = $3, rules = $4
			WHERE project_name = $5 AND rel_path = $6
		`, adrs[idx].ID, adrs[idx].Scope, adrs[idx].SimilarityThreshold, adrs[idx].Rules, s.projectName, adrs[idx].RelPath)
	}

	br := s.pool.SendBatch(ctx, batch)
	for _, idx := range toSync {
		tag, err := br.Exec()
		if err != nil {
			_ = br.Close() //nolint:errcheck // cleanup; the Exec error wins
			return fmt.Errorf("failed to sync metadata for ADR %s: %w", adrs[idx].RelPath, err)
		}

		if tag.RowsAffected() == 0 {
			s.out.Warn("sync UPDATE for %s affected 0 rows (row may have been deleted concurrently)", adrs[idx].RelPath)
		}
	}

	if err := br.Close(); err != nil {
		return fmt.Errorf("failed to close sync batch: %w", err)
	}

	return nil
}

func (s *PgStore) deleteRemoved(ctx context.Context, existing map[string]ADR, valid []ADR) ([]string, error) {
	current := make(map[string]bool, len(valid))
	for _, adr := range valid {
		current[adr.RelPath] = true
	}

	var removed []string

	for relPath := range existing {
		if !current[relPath] {
			removed = append(removed, relPath)
		}
	}

	if len(removed) == 0 {
		return nil, nil
	}

	s.out.Info("Deleting %d removed ADRs from database...", len(removed))

	for _, relPath := range removed {
		if _, err := s.pool.Exec(ctx, "DELETE FROM archguard_adrs WHERE project_name = $1 AND rel_path = $2", s.projectName, relPath); err != nil {
			return nil, fmt.Errorf("failed to delete ADR %s: %w", relPath, err)
		}
	}

	return removed, nil
}

func (s *PgStore) reindexIfChurned(ctx context.Context, modified, total int) {
	if !s.reindexEnabled() {
		return
	}

	threshold := s.reindexThreshold()
	if total == 0 || float64(modified)/float64(total) < threshold {
		return
	}

	mode := "blocking"
	if s.reindexConcurrently() {
		mode = "concurrently"
	}

	s.out.Info("Modifications exceeded %.0f%% threshold. Rebuilding HNSW index (%s)...", threshold*100, mode)

	if _, err := s.pool.Exec(ctx, s.reindexStatement()); err != nil {
		s.out.Warn("failed to reindex HNSW graph: %v", err)
	}
}

// SearchQuery is exported so pgvector_bench_test.go can EXPLAIN this exact
// query, instead of a copy that could drift; scope/threshold filtering happens in Go, not SQL.
const SearchQuery = `
	SELECT rel_path, title, status, content, COALESCE(adr_id, '') AS adr_id, COALESCE(scope, '') AS scope, similarity_threshold, rules, (1 - (embedding <=> $1)) as similarity
	FROM archguard_adrs
	WHERE project_name = $2
	ORDER BY embedding <=> $1
	LIMIT $3
`

// Scope is a glob Postgres can't evaluate, so it is filtered in Go.
const scopedADRsQuery = `
	SELECT rel_path, title, status, content, COALESCE(adr_id, '') AS adr_id, COALESCE(scope, '') AS scope, similarity_threshold, rules
	FROM archguard_adrs
	WHERE project_name = $1
	ORDER BY rel_path
`

// MaxSearchCandidates bounds each PgStore search's fetch
// (nearest rows by distance) so Go-side filtering sees every candidate.
const MaxSearchCandidates = 1000

func scanSearchResults(rows pgx.Rows, out *output.Printer) []SearchResult {
	var candidates []SearchResult
	for rows.Next() {
		var adr ADR

		var score float64
		if err := rows.Scan(&adr.RelPath, &adr.Title, &adr.Status, &adr.Content, &adr.ID, &adr.Scope, &adr.SimilarityThreshold, &adr.Rules, &score); err != nil {
			out.Error("PgStore Row scan failed: %v", err)
			continue
		}

		candidates = append(candidates, SearchResult{ADR: &adr, Score: score})
	}

	return candidates
}

func (s *PgStore) ScopedADRs(filePath string) ([]SearchResult, error) {
	adrs, err := s.projectADRs()
	if err != nil {
		return nil, err
	}

	candidates := make([]SearchResult, 0, len(adrs))
	for i := range adrs {
		candidates = append(candidates, SearchResult{ADR: &adrs[i]})
	}

	return filterByScope(candidates, filePath), nil
}

// Cached so a check run reads the corpus once, not once per file; BuildIndex drops it.
func (s *PgStore) projectADRs() ([]ADR, error) {
	s.adrsMu.Lock()
	defer s.adrsMu.Unlock()

	if s.adrsLoaded {
		return s.adrs, nil
	}

	rows, err := s.pool.Query(context.Background(), scopedADRsQuery, s.projectName)
	if err != nil {
		return nil, fmt.Errorf("querying ADRs: %w", err)
	}
	defer rows.Close()

	var adrs []ADR
	for rows.Next() {
		var adr ADR
		if err := rows.Scan(&adr.RelPath, &adr.Title, &adr.Status, &adr.Content, &adr.ID, &adr.Scope, &adr.SimilarityThreshold, &adr.Rules); err != nil {
			return nil, fmt.Errorf("scanning ADR row: %w", err)
		}

		adrs = append(adrs, adr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading ADR rows: %w", err)
	}

	s.adrs, s.adrsLoaded = adrs, true
	return adrs, nil
}

func (s *PgStore) dropADRCache() {
	s.adrsMu.Lock()
	defer s.adrsMu.Unlock()
	s.adrs, s.adrsLoaded = nil, false
}

func (s *PgStore) Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	rows, err := s.pool.Query(ctx, SearchQuery, vec, s.projectName, MaxSearchCandidates)
	if err != nil {
		s.out.Error("PgStore Search query failed: %v", err)
		return nil
	}
	defer rows.Close()

	candidates := scanSearchResults(rows, s.out)
	candidates = filterByScope(candidates, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
}

func (s *PgStore) SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	rows, err := s.pool.Query(ctx, SearchQuery, vec, s.projectName, MaxSearchCandidates)
	if err != nil {
		s.out.Error("PgStore SearchRejected query failed: %v", err)
		return nil
	}
	defer rows.Close()

	candidates := scanSearchResults(rows, s.out)
	candidates = filterByScope(candidates, filePath)
	candidates = filterBelowThreshold(candidates, threshold)
	return rankAndLimit(candidates, topK)
}

func (s *PgStore) SearchTruncated(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	rows, err := s.pool.Query(ctx, SearchQuery, vec, s.projectName, MaxSearchCandidates)
	if err != nil {
		s.out.Error("PgStore SearchTruncated query failed: %v", err)
		return nil
	}
	defer rows.Close()

	candidates := scanSearchResults(rows, s.out)
	candidates = filterByScope(candidates, filePath)
	candidates = filterByThreshold(candidates, threshold)
	return truncatedByTopK(candidates, topK)
}

func (s *PgStore) SearchWithDebugInfo(queryEmbedding []float32, threshold float64, topK int, filePath string) (hits, rejected, truncated []SearchResult) {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	rows, err := s.pool.Query(ctx, SearchQuery, vec, s.projectName, MaxSearchCandidates)
	if err != nil {
		s.out.Error("PgStore SearchWithDebugInfo query failed: %v", err)
		return nil, nil, nil
	}
	defer rows.Close()

	candidates := filterByScope(scanSearchResults(rows, s.out), filePath)

	belowCopy := append([]SearchResult(nil), candidates...)
	rejected = rankAndLimit(filterBelowThreshold(belowCopy, threshold), topK)

	qualifying := filterByThreshold(append([]SearchResult(nil), candidates...), threshold)
	hits = rankAndLimit(qualifying, topK)
	truncated = truncatedByTopK(qualifying, topK)

	return hits, rejected, truncated
}
