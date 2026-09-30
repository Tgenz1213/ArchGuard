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
	"github.com/tgenz1213/archguard/internal/output"
	"golang.org/x/sync/errgroup"
)

type SkippedADR struct {
	RelPath string
	Err     error
}

// BuildIndexResult reports the outcome of a BuildIndex run. Skipped can be
// non-empty whether or not BuildIndex also returns an error.
type BuildIndexResult struct {
	IndexSummary
	Skipped []SkippedADR
	// Attempted is false only when BuildIndex failed before fetching ADRs,
	// distinguishing that from a fetch that genuinely found nothing.
	Attempted bool
}

type VectorStore interface {
	// ScopedADRs needs no embedding, so pipelines without a cosine stage never embed.
	ScopedADRs(filePath string) ([]SearchResult, error)
	CalculateHash(adrs []ADR, modelName string) (string, error)
	Load(path, modelName string, dim int, currentHash string) error
	Save(path string) error
	BuildIndex(ctx context.Context, modelName string, dim int, provider llm.Provider, adrProvider Provider) (BuildIndexResult, error)
	// Applies scope, then threshold, then the topK cut.
	Search(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult
	// Scope-matched candidates below threshold. Debug only: callers must gate it behind `if debug`.
	SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult
	// Threshold-passing candidates cut only by topK. Debug only: callers must gate it behind `if debug`.
	SearchTruncated(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult
	// One query yields all three sets, so they can't disagree under hnsw.iterative_scan=relaxed_order
	// (docs/arch/0005). Debug only: callers must gate it behind `if debug`.
	SearchWithDebugInfo(queryEmbedding []float32, threshold float64, topK int, filePath string) (hits, rejected, truncated []SearchResult)
}

type LocalStore struct {
	ADRs        []ADR           `json:"adrs"`
	Hash        string          `json:"hash"`
	ModelName   string          `json:"model_name"`
	Dim         int             `json:"dim"`
	concurrency int             `json:"-"`
	out         *output.Printer `json:"-"`
}

func NewLocalStore(concurrency int) *LocalStore {
	return &LocalStore{
		ADRs:        []ADR{},
		concurrency: concurrency,
	}
}

func NewVectorStore(cfg *config.Config, out *output.Printer) (VectorStore, error) {
	if cfg.VectorStore.ConnectionString != "" {
		return NewPgStore(cfg.VectorStore.ConnectionString, cfg.ProjectName, cfg.VectorStore.EmbeddingConcurrency, HNSWOptions{
			Enabled:       cfg.VectorStore.ReindexEnabled,
			Threshold:     cfg.VectorStore.ReindexThreshold,
			Concurrently:  cfg.VectorStore.ReindexConcurrently,
			IterativeScan: cfg.VectorStore.IterativeScan,
		}, out)
	}

	store := NewLocalStore(cfg.VectorStore.EmbeddingConcurrency)
	store.out = out
	return store, nil
}

// Covers the model name and each ADR's RelPath, Content, ID and rules; changes to other
// fields don't trigger a rebuild. Rules are only hashed when present so older indexes stay valid.
func (s *LocalStore) CalculateHash(adrs []ADR, modelName string) (string, error) {
	hasher := sha256.New()
	hasher.Write([]byte(modelName))

	for _, adr := range adrs {
		hasher.Write([]byte(adr.RelPath))
		hasher.Write([]byte(adr.Content))
		hasher.Write([]byte(adr.ID))

		if len(adr.Rules) > 0 {
			rules, err := json.Marshal(adr.Rules)
			if err != nil {
				return "", err
			}

			hasher.Write(rules)
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *LocalStore) Load(path, modelName string, dim int, currentHash string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("index file not found: %s", path)
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

func (s *LocalStore) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return atomicfile.Write(path, data)
}

func (s *LocalStore) BuildIndex(ctx context.Context, modelName string, dim int, provider llm.Provider, adrProvider Provider) (BuildIndexResult, error) {
	validADRs, stats, err := adrProvider.GetADRs(ctx)
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

	s.out.Info("Found %d valid ADRs. Generating embeddings for %d new/modified ADRs...", len(validADRs), len(adrsToEmbed))

	result := BuildIndexResult{IndexSummary: summarizeCorpus(validADRs, stats), Attempted: true}
	failed := make(map[int]bool)

	if len(adrsToEmbed) > 0 {
		concurrency := s.concurrency
		if concurrency <= 0 {
			concurrency = 5
		}

		var mu sync.Mutex
		g := new(errgroup.Group)
		g.SetLimit(concurrency)
		progress := s.out.Progress()

		markFailed := func(idx int, err error) {
			mu.Lock()
			failed[idx] = true
			result.Skipped = append(result.Skipped, SkippedADR{RelPath: validADRs[idx].RelPath, Err: err})
			mu.Unlock()
			s.out.Warn("skipping ADR %s: %v", validADRs[idx].RelPath, err)
		}

		for _, idx := range adrsToEmbed {
			idx := idx
			g.Go(func() error {
				textToEmbed := fmt.Sprintf("Title: %s\nStatus: %s\nContent: %s", validADRs[idx].Title, validADRs[idx].Status, validADRs[idx].Content)

				emb, embErr := provider.CreateEmbedding(ctx, textToEmbed, llm.EmbeddingTaskDocument)
				if embErr != nil {
					markFailed(idx, embErr)
					return nil
				}

				validADRs[idx].Embedding = emb
				progress.Tick()
				return nil
			})
		}

		err := g.Wait()
		progress.Done()

		if err != nil {
			return result, err
		}
	}

	// Valid means successfully indexed, not merely status-accepted.
	result.Valid = len(validADRs) - len(failed)

	// Checked unconditionally: a ctx canceled before a no-embed run (every
	// ADR unchanged) must still surface, not fall through as success.
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	if len(validADRs) > 0 && len(failed) == len(validADRs) {
		return result, fmt.Errorf("all %d ADR(s) failed to embed; index not updated", len(validADRs))
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

	// Hash the full fetched set, not finalADRs: the hash answers "does this
	// index match the ADR files on disk", and a skipped ADR is still on disk.
	hash, err := s.CalculateHash(validADRs, modelName)
	if err != nil {
		return result, fmt.Errorf("failed to calculate hash: %w", err)
	}

	s.Hash = hash

	return result, nil
}
