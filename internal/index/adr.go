package index

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type ADR struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Status string        `json:"status"`
	Scope  ScopePatterns `json:"scope"` // Optional glob pattern(s) from frontmatter
	// SimilarityThreshold overrides vector_store.similarity_threshold for
	// this ADR only; nil means "use the global value" (see EffectiveThreshold).
	SimilarityThreshold *float64  `json:"similarity_threshold,omitempty"`
	Content             string    `json:"content"`
	Embedding           []float32 `json:"embedding"`
	RelPath             string    `json:"rel_path"`
}

type FrontMatter struct {
	Title               string        `yaml:"title"`
	Status              string        `yaml:"status"`
	Scope               ScopePatterns `yaml:"scope"`
	SimilarityThreshold *float64      `yaml:"similarity_threshold"`
}

// CanonicalFrontMatterFields lists the FrontMatter fields that
// analysis.frontmatter_mappings may remap to a different YAML key.
var CanonicalFrontMatterFields = []string{"title", "status", "scope", "similarity_threshold"}

// ScopePatterns holds one or more glob patterns from an ADR's scope
// frontmatter, matched with OR semantics; nil/empty means unrestricted.
type ScopePatterns []string

// Matches reports whether filePath matches any pattern, or true if sp is empty.
func (sp ScopePatterns) Matches(filePath string) bool {
	if len(sp) == 0 {
		return true
	}
	for _, pattern := range sp {
		if MatchGlob(pattern, filePath) {
			return true
		}
	}
	return false
}

// Serialize renders sp for TEXT-column storage: a lone pattern as raw text
// (matching pre-existing PgStore rows), multiple patterns as a JSON array.
func (sp ScopePatterns) Serialize() string {
	switch len(sp) {
	case 0:
		return ""
	case 1:
		return sp[0]
	default:
		data, _ := json.Marshal([]string(sp))
		return string(data)
	}
}

// ParseScopePatterns is Serialize's inverse: a JSON array parses as
// multiple patterns, anything else (including legacy raw text) as one.
// Only a "["-prefixed value is treated as JSON -- Serialize never emits any
// other JSON shape, so a literal pattern like "null" isn't misread as one.
func ParseScopePatterns(s string) ScopePatterns {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(s), "[") {
		var patterns []string
		if err := json.Unmarshal([]byte(s), &patterns); err == nil {
			return ScopePatterns(patterns)
		}
	}
	return ScopePatterns{s}
}

func (sp ScopePatterns) MarshalJSON() ([]byte, error) {
	if len(sp) <= 1 {
		return json.Marshal(sp.Serialize())
	}
	return json.Marshal([]string(sp))
}

func (sp *ScopePatterns) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*sp = ParseScopePatterns(single)
		return nil
	}
	var multi []string
	if err := json.Unmarshal(data, &multi); err != nil {
		return err
	}
	*sp = ScopePatterns(multi)
	return nil
}

func (sp *ScopePatterns) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		if s == "" {
			*sp = nil
		} else {
			*sp = ScopePatterns{s}
		}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*sp = ScopePatterns(list)
		return nil
	case 0:
		*sp = nil
		return nil
	default:
		return fmt.Errorf("scope must be a string or a list of strings")
	}
}

// Value and Scan let ScopePatterns act as a pgx/database-sql query
// parameter and scan destination directly against a TEXT column.
func (sp ScopePatterns) Value() (driver.Value, error) {
	return sp.Serialize(), nil
}

func (sp *ScopePatterns) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*sp = nil
	case string:
		*sp = ParseScopePatterns(v)
	case []byte:
		*sp = ParseScopePatterns(string(v))
	default:
		return fmt.Errorf("unsupported scan type %T for ScopePatterns", src)
	}
	return nil
}

func ParseADR(path string, rootDir string, idPattern *regexp.Regexp, frontmatterMappings map[string]string) (*ADR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	relPath, _ := filepath.Rel(rootDir, path)
	filename := filepath.Base(path)
	id := extractID(filename, idPattern)

	return ParseADRContent(data, id, relPath, frontmatterMappings)
}

// extractID uses idPattern's capture group 1 (or whole match) when it matches;
// otherwise falls back to the first-hyphen split.
func extractID(filename string, idPattern *regexp.Regexp) string {
	if idPattern != nil {
		if m := idPattern.FindStringSubmatch(filename); m != nil {
			id := m[0]
			if len(m) > 1 {
				id = m[1]
			}
			if id != "" {
				return id
			}
		}
	}
	return strings.Split(filename, "-")[0]
}

func ParseADRContent(data []byte, id string, relPath string, frontmatterMappings map[string]string) (*ADR, error) {
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil, fmt.Errorf("no frontmatter found in %s", relPath)
	}

	parts := bytes.SplitN(data, []byte("---"), 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid frontmatter format in %s", relPath)
	}

	fm, err := decodeFrontMatter(parts[1], frontmatterMappings)
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter in %s: %w", relPath, err)
	}

	return &ADR{
		ID:                  id,
		Title:               fm.Title,
		Status:              fm.Status,
		Scope:               fm.Scope,
		SimilarityThreshold: fm.SimilarityThreshold,
		Content:             string(parts[2]),
		RelPath:             relPath,
	}, nil
}

// decodeFrontMatter reads each canonical field from its mapped source key,
// falling back to the canonical key itself when unmapped.
func decodeFrontMatter(raw []byte, frontmatterMappings map[string]string) (FrontMatter, error) {
	var fm FrontMatter
	if len(frontmatterMappings) == 0 {
		if err := yaml.Unmarshal(raw, &fm); err != nil {
			return FrontMatter{}, err
		}
		return fm, nil
	}

	var nodes map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &nodes); err != nil {
		return FrontMatter{}, err
	}

	sourceKey := func(canonical string) string {
		if mapped, ok := frontmatterMappings[canonical]; ok && mapped != "" {
			return mapped
		}
		return canonical
	}

	if node, ok := nodes[sourceKey("title")]; ok {
		if err := node.Decode(&fm.Title); err != nil {
			return FrontMatter{}, err
		}
	}
	if node, ok := nodes[sourceKey("status")]; ok {
		if err := node.Decode(&fm.Status); err != nil {
			return FrontMatter{}, err
		}
	}
	if node, ok := nodes[sourceKey("scope")]; ok {
		if err := node.Decode(&fm.Scope); err != nil {
			return FrontMatter{}, err
		}
	}
	if node, ok := nodes[sourceKey("similarity_threshold")]; ok {
		var threshold float64
		if err := node.Decode(&threshold); err != nil {
			return FrontMatter{}, err
		}
		fm.SimilarityThreshold = &threshold
	}
	return fm, nil
}
