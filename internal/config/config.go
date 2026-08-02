package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version     string      `yaml:"version"`
	ProjectName string      `yaml:"project_name"`
	LLM         LLMConfig   `yaml:"llm"`
	VectorStore VectorStore `yaml:"vector_store"`
	Analysis    Analysis    `yaml:"analysis"`
	IndexFile   string      `yaml:"index_file"` // Optional, defaults to .archguard/index.json
}

type LLMConfig struct {
	Provider     string  `yaml:"provider"`
	Model        string  `yaml:"model"`
	BaseURL      string  `yaml:"base_url"`
	MaxTokens    int     `yaml:"max_tokens"`
	Temperature  float64 `yaml:"temperature"`
	SystemPrompt string  `yaml:"system_prompt"`
}

type VectorStore struct {
	Provider             string  `yaml:"provider"`
	Model                string  `yaml:"model"`
	EmbeddingDim         int     `yaml:"embedding_dim"`
	SimilarityThreshold  float64 `yaml:"similarity_threshold"`
	ConnectionString     string  `yaml:"connection_string"`
	EmbeddingConcurrency int     `yaml:"embedding_concurrency"`
	// ReindexEnabled turns PgStore's automatic HNSW reindex maintenance in
	// BuildIndex on or off. nil (unset in YAML) means enabled -- only an
	// explicit `reindex_enabled: false` turns it off, since a plain bool's
	// zero value can't distinguish "unset" from "explicitly false".
	ReindexEnabled *bool `yaml:"reindex_enabled"`
	// ReindexThreshold is the fraction (0.0-1.0) of churned ADRs (embedded +
	// deleted, relative to total) that triggers a reindex. <= 0 means unset;
	// PgStore falls back to its 0.20 default.
	ReindexThreshold float64 `yaml:"reindex_threshold"`
	// ReindexConcurrently selects REINDEX INDEX CONCURRENTLY (non-blocking)
	// vs. plain REINDEX INDEX (ACCESS EXCLUSIVE lock) when a reindex fires.
	// nil (unset in YAML) means CONCURRENTLY -- only an explicit
	// `reindex_concurrently: false` selects the blocking form.
	ReindexConcurrently *bool `yaml:"reindex_concurrently"`
}

type Confluence struct {
	Enabled  bool   `yaml:"enabled"`
	Domain   string `yaml:"domain"` // e.g., "mycompany.atlassian.net"
	SpaceID  string `yaml:"space_id"`
	Username string `yaml:"username"`
	Token    string `yaml:"token"` // API token
}

type Analysis struct {
	ADRPath          string     `yaml:"adr_path"`
	AcceptedStatuses []string   `yaml:"accepted_statuses"`
	ExcludePatterns  []string   `yaml:"exclude_patterns"`
	MaxConcurrency   int        `yaml:"max_concurrency"`
	Confluence       Confluence `yaml:"confluence"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if envDBURL := os.Getenv("ARCHGUARD_DB_URL"); envDBURL != "" {
		cfg.VectorStore.ConnectionString = envDBURL
	}

	if cfg.VectorStore.EmbeddingConcurrency <= 0 {
		cfg.VectorStore.EmbeddingConcurrency = 5
	}

	return &cfg, nil
}
