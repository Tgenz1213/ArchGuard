package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tgenz1213/archguard/internal/llm"
)

type Cache struct {
	Dir string
}

func NewCache(projectRoot string) (*Cache, error) {
	cacheDir := filepath.Join(projectRoot, ".archguard", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}
	return &Cache{Dir: cacheDir}, nil
}

func (c *Cache) Get(key string) (*llm.AnalysisResult, bool, error) {
	path := filepath.Join(c.Dir, key+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, false, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}

	var res llm.AnalysisResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, false, err // Corrupt cache? Treat as miss.
	}
	return &res, true, nil
}

func (c *Cache) Put(key string, res *llm.AnalysisResult) error {
	path := filepath.Join(c.Dir, key+".json")
	data, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// hashParts length-prefixes each part before hashing so e.g. ("a||b","c")
// can't hash the same as ("a","b||c").
func hashParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(part)))
		h.Write(lenBuf[:])
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type AnalysisKeyInput struct {
	ModelName          string
	ADRContent         string
	FileContent        string
	SystemPrompt       string
	UserPromptTemplate string
}

func ComputeAnalysisKey(in AnalysisKeyInput) string {
	return hashParts(in.ModelName, in.ADRContent, in.FileContent, in.SystemPrompt, in.UserPromptTemplate)
}

// SuggestionKeyInput is a separate namespace from AnalysisKeyInput, keyed
// on the suggestion prompt so changing it invalidates only suggestions.
type SuggestionKeyInput struct {
	ModelName                string
	ADRContent               string
	FileContent              string
	Filename                 string
	Reasoning                string
	QuotedCode               string
	SuggestionSystemPrompt   string
	SuggestionPromptTemplate string
}

func ComputeSuggestionKey(in SuggestionKeyInput) string {
	return hashParts(in.ModelName, in.ADRContent, in.FileContent, in.Filename, in.Reasoning, in.QuotedCode, in.SuggestionSystemPrompt, in.SuggestionPromptTemplate)
}

func (c *Cache) suggestionPath(key string) string {
	return filepath.Join(c.Dir, "suggestions", key+".json")
}

func (c *Cache) GetSuggestion(key string) (string, bool, error) {
	data, err := os.ReadFile(c.suggestionPath(key))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var suggestion string
	if err := json.Unmarshal(data, &suggestion); err != nil {
		return "", false, err // Corrupt cache? Treat as miss.
	}
	return suggestion, true, nil
}

func (c *Cache) PutSuggestion(key, suggestion string) error {
	dir := filepath.Join(c.Dir, "suggestions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(suggestion)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0644)
}
