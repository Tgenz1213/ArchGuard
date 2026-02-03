package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tgenz1213/archguard/internal/llm"
)

// Store manages the persistence and retrieval of ADR embeddings and metadata.
type Store struct {
	ADRs      []ADR  `json:"adrs"`
	Hash      string `json:"hash"`
	ModelName string `json:"model_name"`
	Dim       int    `json:"dim"`
}

// NewStore initializes a new Store instance.
func NewStore() *Store {
	return &Store{
		ADRs: []ADR{},
	}
}

// CalculateHash generates a hash of all ADR file contents and the model name
// to detect if the index needs a rebuild.
func (s *Store) CalculateHash(dirPath, modelName string) (string, error) {
	hasher := sha256.New()
	hasher.Write([]byte(modelName))

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			hasher.Write([]byte(info.Name()))
			content, err := os.ReadFile(path)
			if err == nil {
				hasher.Write(content)
			}
		}
		return nil
	})
	return hex.EncodeToString(hasher.Sum(nil)), err
}

// Load reads the index from disk and validates metadata against the current configuration.
func (s *Store) Load(path, modelName string, dim int, currentHash string) error {
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
func (s *Store) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// BuildIndex crawls the specified directory, parses ADRs, and generates embeddings in parallel.
func (s *Store) BuildIndex(ctx context.Context, dirPath string, modelName string, provider llm.Provider, acceptedStatuses []string) error {
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

	fmt.Printf("Found %d valid ADRs. Generating embeddings...\n", len(validADRs))

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
		validADRs[res.index].Embedding = res.embedding
		fmt.Printf(".")
	}
	fmt.Println()

	s.ADRs = validADRs
	s.ModelName = modelName
	if len(validADRs) > 0 {
		actualDim := len(validADRs[0].Embedding)
		s.Dim = actualDim
		fmt.Printf("Index built with %d dimensions.\n", actualDim)
	}

	hash, err := s.CalculateHash(dirPath, modelName)
	if err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}
	s.Hash = hash

	return nil
}
