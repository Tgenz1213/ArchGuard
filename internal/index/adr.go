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
	ID                  string        `json:"id"`
	Title               string        `json:"title"`
	Status              string        `json:"status"`
	Scope               ScopePatterns `json:"scope"`
	SimilarityThreshold *float64      `json:"similarity_threshold,omitempty"`
	Content             string        `json:"content"`
	Embedding           []float32     `json:"embedding"`
	RelPath             string        `json:"rel_path"`
	Rules               Rules         `json:"rules,omitempty"`
}

type FrontMatter struct {
	Title               string        `yaml:"title"`
	Status              string        `yaml:"status"`
	Scope               ScopePatterns `yaml:"scope"`
	SimilarityThreshold *float64      `yaml:"similarity_threshold"`
	Rules               yaml.Node     `yaml:"rules"`
}

// CanonicalFrontMatterFields lists the FrontMatter fields that
// analysis.frontmatter_mappings may remap to a different YAML key.
var CanonicalFrontMatterFields = []string{"title", "status", "scope", "similarity_threshold", "rules"}

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
		data, _ := json.Marshal([]string(sp)) //nolint:errcheck // marshaling a []string can't fail
		return string(data)
	}
}

// ParseScopePatterns reads a JSON array as many patterns and anything else, legacy raw text
// included, as one; only a "["-prefixed value is tried as JSON so a literal "null" survives.
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

type ParseOptions struct {
	FrontmatterMappings map[string]string
	RulesHeading        string
}

func ParseADR(path string, rootDir string, idPattern *regexp.Regexp, opts ParseOptions) (*ADR, error) {
	adr, _, err := parseADRFile(path, rootDir, idPattern, opts) //nolint:errcheck // malformed rules are reported by providers, not here
	return adr, err
}

func parseADRFile(path string, rootDir string, idPattern *regexp.Regexp, opts ParseOptions) (adr *ADR, rulesErr error, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	relPath, err := filepath.Rel(rootDir, path)
	if err != nil {
		return nil, nil, err
	}

	filename := filepath.Base(path)
	id := extractID(filename, idPattern)

	return parseADR(data, id, relPath, opts, nil)
}

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

func ParseADRContent(data []byte, id string, relPath string, opts ParseOptions) (*ADR, error) {
	adr, _, err := parseADR(data, id, relPath, opts, nil) //nolint:errcheck // malformed rules are reported by providers, not here
	return adr, err
}

// The closing fence is the first line starting with "---", so a "---" inside a value can't end the
// frontmatter; YAML lines never start with it.
func splitFrontMatter(data []byte) (frontMatter, body []byte, ok bool) {
	end := bytes.Index(data[3:], []byte("\n---"))
	if end < 0 {
		return nil, nil, false
	}

	closing := 3 + end + 1

	return data[3:closing], data[closing+3:], true
}

// rulesErr means the ADR is usable but its rules were dropped; err means the ADR is unusable.
func parseADR(data []byte, id string, relPath string, opts ParseOptions, contentOverride *string) (adr *ADR, rulesErr error, err error) {
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil, nil, fmt.Errorf("no frontmatter found in %s", relPath)
	}

	frontMatter, body, ok := splitFrontMatter(data)
	if !ok {
		return nil, nil, fmt.Errorf("invalid frontmatter format in %s", relPath)
	}

	fm, err := decodeFrontMatter(frontMatter, opts.FrontmatterMappings)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse frontmatter in %s: %w", relPath, err)
	}

	content := string(body)
	if contentOverride != nil {
		content = *contentOverride
	}

	heading := strings.TrimSpace(opts.RulesHeading)
	if heading == "" {
		heading = DefaultRulesHeading
	}

	rules, rulesErr := frontMatterRules(&fm.Rules)
	if rulesErr == nil && len(rules) == 0 {
		rules, rulesErr = bodyRules(content, heading)
	}

	return &ADR{
		ID:                  id,
		Title:               fm.Title,
		Status:              fm.Status,
		Scope:               fm.Scope,
		SimilarityThreshold: fm.SimilarityThreshold,
		Content:             content,
		RelPath:             relPath,
		Rules:               rules,
	}, rulesErr, nil
}

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
		if err := node.Decode(&fm.SimilarityThreshold); err != nil {
			return FrontMatter{}, err
		}
	}

	if node, ok := nodes[sourceKey("rules")]; ok {
		fm.Rules = node
	}

	return fm, nil
}
