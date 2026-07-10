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
	Provider            string  `yaml:"provider"`
	Model               string  `yaml:"model"`
	EmbeddingDim        int     `yaml:"embedding_dim"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"`
	ConnectionString    string  `yaml:"connection_string"`
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

	return &cfg, nil
}
