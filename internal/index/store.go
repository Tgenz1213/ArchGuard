package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tgenz1213/archguard/internal/atomicfile"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type SkippedADR struct {
	RelPath string
	Err     error
}

// Skipped can be non-empty even when BuildIndex also returns an error.
type BuildIndexResult struct {
	Summary
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
	BuildIndex(ctx context.Context, modelName string, dim int, embedder inference.Embedder, adrProvider Provider) (BuildIndexResult, error)
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
	ADRs        []ADR  `json:"adrs"`
	Hash        string `json:"hash"`
	ModelName   string `json:"model_name"`
	Dim         int    `json:"dim"`
	concurrency int
	out         *output.Printer
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
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("index file not found: %s", path)
		}

		return err
	}

	if err := json.Unmarshal(raw, s); err != nil {
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
	encoded, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return atomicfile.Write(path, encoded)
}

func (s *LocalStore) BuildIndex(ctx context.Context, modelName string, dim int, embedder inference.Embedder, adrProvider Provider) (BuildIndexResult, error) {
	validADRs, stats, err := adrProvider.GetADRs(ctx)
	if err != nil {
		return BuildIndexResult{}, err
	}

	adrsToEmbed := s.reuseUnchangedEmbeddings(validADRs)

	s.out.Info("Found %d valid ADRs. Generating embeddings for %d new/modified ADRs...", len(validADRs), len(adrsToEmbed))

	result := BuildIndexResult{Summary: summarizeCorpus(validADRs, stats), Attempted: true}
	job := embedJob{adrs: validADRs, embedder: embedder, concurrency: s.concurrency, out: s.out}

	outcome, err := job.run(ctx, adrsToEmbed)
	result.Skipped = outcome.skipped

	if err != nil {
		return result, err
	}

	// Valid means successfully indexed, not merely status-accepted.
	result.Valid = len(validADRs) - len(outcome.failed)

	if err := outcome.check(ctx, len(validADRs), "embed"); err != nil {
		return result, err
	}

	s.replaceADRs(validADRs, outcome.failed, modelName, dim)

	// Hash the full fetched set, not the embedded subset: a skipped ADR is still on disk.
	hash, err := s.CalculateHash(validADRs, modelName)
	if err != nil {
		return result, fmt.Errorf("failed to calculate hash: %w", err)
	}

	s.Hash = hash

	return result, nil
}

func (s *LocalStore) reuseUnchangedEmbeddings(adrs []ADR) []int {
	existing := make(map[string]ADR, len(s.ADRs))
	for _, stored := range s.ADRs {
		existing[stored.RelPath] = stored
	}

	var toEmbed []int

	for i, adr := range adrs {
		if stored, ok := existing[adr.RelPath]; ok && adrUnchanged(stored, adr) {
			adrs[i].Embedding = stored.Embedding
		} else {
			toEmbed = append(toEmbed, i)
		}
	}

	return toEmbed
}

func (s *LocalStore) replaceADRs(adrs []ADR, failed map[int]bool, modelName string, dim int) {
	kept := make([]ADR, 0, len(adrs))

	for i, adr := range adrs {
		if !failed[i] {
			kept = append(kept, adr)
		}
	}

	s.ADRs = kept
	s.ModelName = modelName

	if dim > 0 {
		s.Dim = dim
	} else if len(kept) > 0 && len(kept[0].Embedding) > 0 {
		s.Dim = len(kept[0].Embedding)
	}
}
