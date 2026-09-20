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
	IndexFile   string      `yaml:"index_file"`
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
	Provider             string   `yaml:"provider"`
	Model                string   `yaml:"model"`
	EmbeddingDim         int      `yaml:"embedding_dim"`
	SimilarityThreshold  float64  `yaml:"similarity_threshold"`
	ConnectionString     string   `yaml:"connection_string"`
	EmbeddingConcurrency int      `yaml:"embedding_concurrency"`
	ReindexEnabled       *bool    `yaml:"reindex_enabled"`      // nil (unset) = enabled; only explicit false disables
	ReindexThreshold     *float64 `yaml:"reindex_threshold"`    // nil (unset) = PgStore's 0.20 default; explicit 0.0 reindexes on any churn
	ReindexConcurrently  *bool    `yaml:"reindex_concurrently"` // nil (unset) = CONCURRENTLY; only explicit false uses blocking REINDEX
	IterativeScan        *bool    `yaml:"iterative_scan"`       // nil (unset) = enabled when pgvector supports it; only explicit false disables
}

type Confluence struct {
	Enabled  bool   `yaml:"enabled"`
	Domain   string `yaml:"domain"` // e.g., "mycompany.atlassian.net"
	SpaceID  string `yaml:"space_id"`
	Username string `yaml:"username"`
	Token    string `yaml:"token"`
}

type Analysis struct {
	ADRPath          string     `yaml:"adr_path"`
	AcceptedStatuses []string   `yaml:"accepted_statuses"`
	ExcludePatterns  []string   `yaml:"exclude_patterns"`
	MaxConcurrency   int        `yaml:"max_concurrency"`
	MaxRelevantADRs  int        `yaml:"max_relevant_adrs"`
	Confluence       Confluence `yaml:"confluence"`
	// Regexp applied to an ADR filename to derive its ID; see docs/arch/0012.
	ADRIDPattern string `yaml:"adr_id_pattern"`
	// Canonical field name -> the YAML key the corpus uses; see docs/arch/0021.
	FrontmatterMappings map[string]string `yaml:"frontmatter_mappings"`
	Pipeline            *Pipeline         `yaml:"pipeline"`
}

func (a Analysis) RelevantADRLimit() int {
	if a.MaxRelevantADRs <= 0 {
		return 3
	}
	return a.MaxRelevantADRs
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
