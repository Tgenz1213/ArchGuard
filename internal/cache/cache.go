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
