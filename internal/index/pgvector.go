package index

import (
	"context"
	"fmt"

	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
	"github.com/tgenz1213/archguard/internal/llm"
)

// PgStore implements the VectorStore interface using PostgreSQL and pgvector.
type PgStore struct {
	pool             *pgxpool.Pool
	connectionString string
	projectName      string
	concurrency      int
}

// NewPgStore initializes a new PgStore connected to the given database URL.
func NewPgStore(connStr string, projectName string, concurrency int) (*PgStore, error) {
	ctx := context.Background()

	// Ensure the vector extension exists BEFORE setting up the pool
	tempConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to initially connect to database: %w", err)
	}
	_, err = tempConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	_ = tempConn.Close(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create vector extension: %w", err)
	}

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
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
	}, nil
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
		CREATE INDEX IF NOT EXISTS archguard_adrs_embedding_idx ON archguard_adrs USING hnsw (embedding vector_cosine_ops);
	`, dim)

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
		type result struct {
			index     int
			embedding []float32
			err       error
		}
		results := make(chan result, len(adrsToEmbed))
		var wg sync.WaitGroup

		concurrency := s.concurrency
		if concurrency <= 0 {
			concurrency = 5
		}
		sem := make(chan struct{}, concurrency)

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		for _, idx := range adrsToEmbed {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				textToEmbed := fmt.Sprintf("Title: %s\nStatus: %s\nContent: %s", validADRs[i].Title, validADRs[i].Status, validADRs[i].Content)
				emb, err := provider.CreateEmbedding(ctx, textToEmbed)
				results <- result{index: i, embedding: emb, err: err}
			}(idx)
		}

		go func() {
			wg.Wait()
			close(results)
		}()

		for res := range results {
			if res.err != nil {
				cancel() // immediately cancel remaining API requests
				return fmt.Errorf("failed to embed ADR %s: %w", validADRs[res.index].RelPath, res.err)
			}

			vec := pgvector.NewVector(res.embedding)
			_, err := s.pool.Exec(ctx, `
				INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (project_name, rel_path) DO UPDATE SET
					title = EXCLUDED.title,
					status = EXCLUDED.status,
					content = EXCLUDED.content,
					embedding = EXCLUDED.embedding
			`, s.projectName, validADRs[res.index].RelPath, validADRs[res.index].Title, validADRs[res.index].Status, validADRs[res.index].Content, vec)
			if err != nil {
				return fmt.Errorf("failed to upsert ADR %s: %w", validADRs[res.index].RelPath, err)
			}
			fmt.Printf(".")
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
	modifiedCount := len(adrsToEmbed) + len(toDelete)
	totalCount := len(validADRs) + len(toDelete)
	if totalCount > 0 && float64(modifiedCount)/float64(totalCount) >= 0.20 {
		fmt.Println("Modifications exceeded 20% threshold. Rebuilding HNSW index...")
		_, err := s.pool.Exec(ctx, "REINDEX INDEX archguard_adrs_embedding_idx")
		if err != nil {
			fmt.Printf("Warning: failed to reindex HNSW graph: %v\n", err)
		}
	}

	return nil
}

// Search performs a vector similarity search across the Postgres store using cosine distance.
func (s *PgStore) Search(queryEmbedding []float32, threshold float64, topK int) []SearchResult {
	ctx := context.Background()
	vec := pgvector.NewVector(queryEmbedding)

	// pgvector uses <=> for cosine distance. Similarity is 1 - distance.
	// So similarity >= threshold means distance <= 1 - threshold.
	distanceThreshold := 1.0 - threshold

	query := `
		SELECT rel_path, title, status, content, (1 - (embedding <=> $1)) as similarity
		FROM archguard_adrs
		WHERE project_name = $2 AND embedding <=> $1 <= $3
		ORDER BY embedding <=> $1
		LIMIT $4
	`
	rows, err := s.pool.Query(ctx, query, vec, s.projectName, distanceThreshold, topK)
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
