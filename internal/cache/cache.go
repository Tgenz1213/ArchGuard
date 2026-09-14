package cache

import (
	"crypto/sha256"
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

func ComputeAnalysisKey(modelName, adrContent, fileContent, systemPrompt, userPromptTemplate string) string {
	h := sha256.New()
	h.Write([]byte(modelName))
	h.Write([]byte("||"))
	h.Write([]byte(adrContent))
	h.Write([]byte("||"))
	h.Write([]byte(fileContent))
	h.Write([]byte("||"))
	h.Write([]byte(systemPrompt))
	h.Write([]byte("||"))
	h.Write([]byte(userPromptTemplate))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

// ComputeSuggestionKey is a separate namespace from ComputeAnalysisKey, keyed
// on the suggestion prompt so changing it invalidates only suggestions.
func ComputeSuggestionKey(modelName, adrContent, fileContent, reasoning, quotedCode, suggestionSystemPrompt, suggestionPromptTemplate string) string {
	h := sha256.New()
	for _, part := range []string{modelName, adrContent, fileContent, reasoning, quotedCode, suggestionSystemPrompt, suggestionPromptTemplate} {
		h.Write([]byte(part))
		h.Write([]byte("||"))
	}
	return hex.EncodeToString(h.Sum(nil))
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
