package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

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
	// Embedded excludes reused, dropped and failed ADRs.
	Embedded int
	Skipped  []SkippedADR
	// Attempted is false only when BuildIndex failed before fetching ADRs,
	// distinguishing that from a fetch that genuinely found nothing.
	Attempted bool
}

type VectorStore interface {
	// ScopedADRs needs no embedding, so pipelines without a cosine node never embed.
	ScopedADRs(filePath string) ([]SearchResult, error)
	IndexedPaths() ([]string, error)
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
	ADRs            []ADR  `json:"adrs"`
	Hash            string `json:"hash"`
	ModelName       string `json:"model_name"`
	Dim             int    `json:"dim"`
	EmbeddingFormat string `json:"embedding_format"`
	concurrency     int
	sections        SectionSelection
	out             *output.Printer
}

func NewLocalStore(concurrency int) *LocalStore {
	return &LocalStore{
		ADRs:        []ADR{},
		concurrency: concurrency,
	}
}

func NewVectorStore(cfg *config.Config, out *output.Printer) (VectorStore, error) {
	sections := NewSectionSelection(cfg.Analysis.IncludedHeaders, cfg.Analysis.ExcludedHeaders)

	if cfg.VectorStore.ConnectionString != "" {
		store, err := NewPgStore(
			cfg.VectorStore.ConnectionString,
			cfg.ProjectName,
			cfg.VectorStore.EmbeddingConcurrency,
			HNSWOptions{
				Enabled:       cfg.VectorStore.ReindexEnabled,
				Threshold:     cfg.VectorStore.ReindexThreshold,
				Concurrently:  cfg.VectorStore.ReindexConcurrently,
				IterativeScan: cfg.VectorStore.IterativeScan,
			},
			out,
		)
		if err != nil {
			return nil, err
		}

		store.sections = sections

		return store, nil
	}

	store := NewLocalStore(cfg.VectorStore.EmbeddingConcurrency)
	store.out = out
	store.sections = sections

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

var (
	ErrIndexMissing           = errors.New("index missing")
	ErrEmbeddingModelChanged  = errors.New("embedding model changed")
	ErrEmbeddingFormatChanged = errors.New("embedding format changed")
	ErrADRsChanged            = errors.New("ADRs changed")
)

func (s *LocalStore) Load(path, modelName string, dim int, currentHash string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrIndexMissing, path)
		}

		return err
	}

	if err := json.Unmarshal(raw, s); err != nil {
		return err
	}

	var reasons []error
	if s.ModelName != modelName {
		reasons = append(reasons, fmt.Errorf("%w: saved %q, config %q", ErrEmbeddingModelChanged, s.ModelName, modelName))
	}

	if s.Dim != dim {
		reasons = append(reasons, fmt.Errorf("%w: saved dimension %d, config %d", ErrEmbeddingModelChanged, s.Dim, dim))
	}

	if format := s.sections.format(); s.EmbeddingFormat != format {
		reasons = append(reasons, fmt.Errorf("%w: saved %q, current %q", ErrEmbeddingFormatChanged, s.EmbeddingFormat, format))
	}

	// The hash covers the model name, so after a model change it can't tell whether the ADRs changed too.
	if s.Hash != currentHash && s.ModelName == modelName {
		reasons = append(reasons, fmt.Errorf("%w: saved hash %s, current %s", ErrADRsChanged, s.Hash, currentHash))
	}

	return errors.Join(reasons...)
}

// Unlike Load, it accepts a stale index, since BuildIndex reuses only the embeddings of unchanged ADRs.
func (s *LocalStore) LoadSaved(path string) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return err
	}

	var saved LocalStore
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}

	s.ADRs, s.ModelName, s.Dim, s.EmbeddingFormat = saved.ADRs, saved.ModelName, saved.Dim, saved.EmbeddingFormat

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
	fetchedADRs, stats, err := adrProvider.GetADRs(ctx)
	if err != nil {
		return BuildIndexResult{}, err
	}

	validADRs, unembeddable := dropUnembeddable(fetchedADRs, s.sections, s.out)
	adrsToEmbed := s.reuseUnchangedEmbeddings(validADRs, modelName, dim)

	result := BuildIndexResult{Summary: summarizeCorpus(validADRs, stats), Skipped: unembeddable, Attempted: true}
	job := embedJob{adrs: validADRs, embedder: embedder, concurrency: s.concurrency, sections: s.sections, out: s.out}

	outcome, err := job.embedInto(ctx, adrsToEmbed, &result)
	if err != nil {
		return result, err
	}

	s.replaceADRs(validADRs, outcome.failed, modelName, dim)

	// Hash the full fetched set, not the embedded subset: a skipped ADR is still on disk.
	hash, err := s.CalculateHash(fetchedADRs, modelName)
	if err != nil {
		return result, fmt.Errorf("failed to calculate hash: %w", err)
	}

	s.Hash = hash

	return result, nil
}

func (s *LocalStore) reuseUnchangedEmbeddings(adrs []ADR, modelName string, dim int) []int {
	existing := make(map[string]ADR, len(s.ADRs))
	if s.embeddedWith(modelName, dim) {
		for _, stored := range s.ADRs {
			existing[stored.RelPath] = stored
		}
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

// A dim of 0 means the config leaves it unset, so only the model name can be compared.
func (s *LocalStore) embeddedWith(modelName string, dim int) bool {
	return s.EmbeddingFormat == s.sections.format() && s.ModelName == modelName && (dim == 0 || s.Dim == dim)
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
	s.EmbeddingFormat = s.sections.format()

	if dim > 0 {
		s.Dim = dim
	} else if len(kept) > 0 && len(kept[0].Embedding) > 0 {
		s.Dim = len(kept[0].Embedding)
	}
}
