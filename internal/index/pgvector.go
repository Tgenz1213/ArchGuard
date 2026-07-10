package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
}

// NewPgStore initializes a new PgStore connected to the given database URL.
func NewPgStore(connStr string, projectName string) (*PgStore, error) {
	ctx := context.Background()

	// Ensure the vector extension exists BEFORE setting up the pool
	tempConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to initially connect to database: %w", err)
	}
	_, err = tempConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	tempConn.Close(ctx)
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
	}, nil
}

// CalculateHash is a no-op for PgStore because the database maintains state incrementally (or is completely truncated on Build).
func (s *PgStore) CalculateHash(dirPath, modelName string) (string, error) {
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
		)
	`, dim)
	
	_, err := s.pool.Exec(ctx, query)
	return err
}

// Save is a no-op for PgStore as data is persisted immediately during BuildIndex.
func (s *PgStore) Save(path string) error {
	return nil
}

// BuildIndex parses the ADRs, generates embeddings, and inserts them into the database.
func (s *PgStore) BuildIndex(ctx context.Context, dirPath string, modelName string, provider llm.Provider, acceptedStatuses []string) error {
	var validADRs []ADR

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			adr, err := ParseADR(path, dirPath)
			if err != nil {
				fmt.Printf("Warning: skipping %s: %v\n", path, err)
				return nil
			}

			for _, status := range acceptedStatuses {
				if strings.EqualFold(strings.TrimSpace(adr.Status), strings.TrimSpace(status)) {
					validADRs = append(validADRs, *adr)
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("Found %d valid ADRs. Inserting into pgvector...\n", len(validADRs))

	// Delete only the ADRs for this specific project
	if _, err := s.pool.Exec(ctx, "DELETE FROM archguard_adrs WHERE project_name = $1", s.projectName); err != nil {
		return fmt.Errorf("failed to clear old ADRs for project %s: %w", s.projectName, err)
	}

	type result struct {
		index     int
		embedding []float32
		err       error
	}
	results := make(chan result, len(validADRs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i := range validADRs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			textToEmbed := fmt.Sprintf("Title: %s\nStatus: %s\nContent: %s", validADRs[i].Title, validADRs[i].Status, validADRs[i].Content)
			emb, err := provider.CreateEmbedding(ctx, textToEmbed)
			results <- result{index: i, embedding: emb, err: err}
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.err != nil {
			return fmt.Errorf("failed to embed ADR %s: %w", validADRs[res.index].RelPath, res.err)
		}
		
		vec := pgvector.NewVector(res.embedding)
		_, err := s.pool.Exec(ctx, `
			INSERT INTO archguard_adrs (project_name, rel_path, title, status, content, embedding)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			s.projectName, validADRs[res.index].RelPath, validADRs[res.index].Title, validADRs[res.index].Status, validADRs[res.index].Content, vec,
		)
		if err != nil {
			return fmt.Errorf("failed to insert ADR %s: %w", validADRs[res.index].RelPath, err)
		}
		fmt.Printf(".")
	}
	fmt.Println()

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
