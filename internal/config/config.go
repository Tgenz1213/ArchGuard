package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version     string      `yaml:"version"`
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
}

type Analysis struct {
	ADRPath          string   `yaml:"adr_path"`
	AcceptedStatuses []string `yaml:"accepted_statuses"`
	ExcludePatterns  []string `yaml:"exclude_patterns"`
	MaxConcurrency   int      `yaml:"max_concurrency"`
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

	return &cfg, nil
}
