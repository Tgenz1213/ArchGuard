package index

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ADR struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Scope     string    `json:"scope"` // Optional glob pattern from frontmatter
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding"`
	RelPath   string    `json:"rel_path"`
}

type FrontMatter struct {
	Title  string `yaml:"title"`
	Status string `yaml:"status"`
	Scope  string `yaml:"scope"`
}

func ParseADR(path string, rootDir string) (*ADR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Split frontmatter
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil, fmt.Errorf("no frontmatter found in %s", path)
	}

	parts := bytes.SplitN(data, []byte("---"), 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid frontmatter format in %s", path)
	}

	var fm FrontMatter
	if err := yaml.Unmarshal(parts[1], &fm); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter in %s: %w", path, err)
	}

	// Basic ID extraction from filename (e.g., 0001-record-architecture.md -> 0001)
	filename := filepath.Base(path)
	id := strings.Split(filename, "-")[0]

	relPath, _ := filepath.Rel(rootDir, path)

	return &ADR{
		ID:      id,
		Title:   fm.Title,
		Status:  fm.Status,
		Scope:   fm.Scope,
		Content: string(parts[2]),
		RelPath: relPath,
	}, nil
}
