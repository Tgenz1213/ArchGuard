package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/tgenz1213/archguard/internal/atomicfile"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
	"golang.org/x/sync/errgroup"
)

// SkippedADR records one ADR that BuildIndex could not embed or persist.
type SkippedADR struct {
	RelPath string
	Err     error
}

// BuildIndexResult reports ADRs BuildIndex could not process. A non-empty
// Skipped does not make BuildIndex return an error.
type BuildIndexResult struct {
	Skipped []SkippedADR
}

// VectorStore defines the interface for interacting with the index storage.
type VectorStore interface {
	CalculateHash(adrs []ADR, modelName string) (string, error)
	Load(path, modelName string, dim int, currentHash string) error
	Save(path string) error
	BuildIndex(ctx context.Context, modelName string, dim int, provider llm.Provider, adrProvider Provider) (BuildIndexResult, error)
	// Search filters candidates by scope, then by threshold, before
	// ranking and cutting to topK -- see filterByScope, filterByThreshold, and rankAndLimit.
	Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult
}

// LocalStore manages the persistence and retrieval of ADR embeddings and metadata.
type LocalStore struct {
	ADRs        []ADR  `json:"adrs"`
	Hash        string `json:"hash"`
	ModelName   string `json:"model_name"`
	Dim         int    `json:"dim"`
	concurrency int    `json:"-"`
}

// NewLocalStore initializes a new LocalStore instance.
func NewLocalStore(concurrency int) *LocalStore {
	return &LocalStore{
		ADRs:        []ADR{},
		concurrency: concurrency,
	}
}

// NewVectorStore creates the appropriate VectorStore based on the configuration.
func NewVectorStore(cfg *config.Config) (VectorStore, error) {
	if cfg.VectorStore.ConnectionString != "" {
		return NewPgStore(cfg.VectorStore.ConnectionString, cfg.ProjectName, cfg.VectorStore.EmbeddingConcurrency, HNSWOptions{
			Enabled:       cfg.VectorStore.ReindexEnabled,
			Threshold:     cfg.VectorStore.ReindexThreshold,
			Concurrently:  cfg.VectorStore.ReindexConcurrently,
			IterativeScan: cfg.VectorStore.IterativeScan,
		})
	}
	return NewLocalStore(cfg.VectorStore.EmbeddingConcurrency), nil
}

// CalculateHash generates a hash of all ADR file contents and the model name
// to detect if the index needs a rebuild.
func (s *LocalStore) CalculateHash(adrs []ADR, modelName string) (string, error) {
	hasher := sha256.New()
	hasher.Write([]byte(modelName))

	for _, adr := range adrs {
		hasher.Write([]byte(adr.RelPath))
		hasher.Write([]byte(adr.Content))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Load reads the index from disk and validates metadata against the current configuration.
func (s *LocalStore) Load(path, modelName string, dim int, currentHash string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, s); err != nil {
		return err
	}

	if s.ModelName != modelName || s.Dim != dim || s.Hash != currentHash {
		var reasons []string
		if s.ModelName != modelName {
			reasons = append(reasons, fmt.Sprintf("Model mismatch (Saved: %q, Config: %q)", s.ModelName, modelName))
		}
		if s.Dim != dim {
			reasons = append(reasons, fmt.Sprintf("Dimension mismatch (Saved: %d, Config: %d)", s.Dim, dim))
		}
		if s.Hash != currentHash {
			reasons = append(reasons, fmt.Sprintf("Hash mismatch\n    Saved:   %s\n    Current: %s", s.Hash, currentHash))
		}
		return fmt.Errorf("index metadata mismatch:\n  %s", strings.Join(reasons, "\n  "))
	}

	return nil
}

// Save persists the current state of the store to a JSON file.
func (s *LocalStore) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return atomicfile.Write(path, data)
}

func (s *LocalStore) BuildIndex(ctx context.Context, modelName string, dim int, provider llm.Provider, adrProvider Provider) (BuildIndexResult, error) {
	validADRs, err := adrProvider.GetADRs(ctx)
	if err != nil {
		return BuildIndexResult{}, err
	}

	existingMap := make(map[string]ADR)
	for _, a := range s.ADRs {
		existingMap[a.RelPath] = a
	}

	var adrsToEmbed []int
	for i, valid := range validADRs {
		existing, ok := existingMap[valid.RelPath]
		if ok && existing.Content == valid.Content && existing.Title == valid.Title && existing.Status == valid.Status {
			validADRs[i].Embedding = existing.Embedding
		} else {
			adrsToEmbed = append(adrsToEmbed, i)
		}
	}

	fmt.Printf("Found %d valid ADRs. Generating embeddings for %d new/modified ADRs...\n", len(validADRs), len(adrsToEmbed))

	var result BuildIndexResult
	failed := make(map[int]bool)

	if len(adrsToEmbed) > 0 {
		concurrency := s.concurrency
		if concurrency <= 0 {
			concurrency = 5
		}

		var mu sync.Mutex
		g := new(errgroup.Group)
		g.SetLimit(concurrency)

		for _, idx := range adrsToEmbed {
			idx := idx
			g.Go(func() error {
				textToEmbed := fmt.Sprintf("Title: %s\nStatus: %s\nContent: %s", validADRs[idx].Title, validADRs[idx].Status, validADRs[idx].Content)
				emb, embErr := provider.CreateEmbedding(ctx, textToEmbed, llm.EmbeddingTaskDocument)
				if embErr != nil {
					mu.Lock()
					failed[idx] = true
					result.Skipped = append(result.Skipped, SkippedADR{RelPath: validADRs[idx].RelPath, Err: embErr})
					mu.Unlock()
					fmt.Printf("\nWarning: skipping ADR %s: %v\n", validADRs[idx].RelPath, embErr)
					return nil
				}
				validADRs[idx].Embedding = emb
				fmt.Printf(".")
				return nil
			})
		}

		_ = g.Wait()
		fmt.Println()
	}

	finalADRs := make([]ADR, 0, len(validADRs))
	for i, adr := range validADRs {
		if failed[i] {
			continue
		}
		finalADRs = append(finalADRs, adr)
	}

	s.ADRs = finalADRs
	s.ModelName = modelName
	if dim > 0 {
		s.Dim = dim
	} else if len(finalADRs) > 0 && len(finalADRs[0].Embedding) > 0 {
		s.Dim = len(finalADRs[0].Embedding)
	}

	hash, err := s.CalculateHash(finalADRs, modelName)
	if err != nil {
		return result, fmt.Errorf("failed to calculate hash: %w", err)
	}
	s.Hash = hash

	return result, nil
}
