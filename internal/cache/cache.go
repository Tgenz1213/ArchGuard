package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tgenz1213/archguard/internal/atomicfile"
	"github.com/tgenz1213/archguard/internal/inference"
)

type Cache struct {
	Dir string
}

func New(projectRoot string) (*Cache, error) {
	cacheDir := filepath.Join(projectRoot, ".archguard", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}

	return &Cache{Dir: cacheDir}, nil
}

func (c *Cache) Get(key string) (*inference.AnalysisResult, bool, error) {
	var result inference.AnalysisResult

	found, err := c.readEntry("", key, &result)
	if err != nil || !found {
		return nil, false, err
	}

	return &result, true, nil
}

func (c *Cache) Put(key string, result *inference.AnalysisResult) error {
	return c.writeEntry("", key, result)
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
	return hashParts(
		in.ModelName,
		in.ADRContent,
		in.FileContent,
		in.Filename,
		in.Reasoning,
		in.QuotedCode,
		in.SuggestionSystemPrompt,
		in.SuggestionPromptTemplate,
	)
}

// QuestionsKeyInput is its own namespace, so changing the question prompt or model regenerates only questions.
type QuestionsKeyInput struct {
	ModelName      string
	ADRContent     string
	SystemPrompt   string
	PromptTemplate string
}

func ComputeQuestionsKey(in QuestionsKeyInput) string {
	return hashParts(in.ModelName, in.ADRContent, in.SystemPrompt, in.PromptTemplate)
}

func (c *Cache) GetSuggestion(key string) (string, bool, error) {
	var suggestion string
	found, err := c.readEntry("suggestions", key, &suggestion)

	return suggestion, found, err
}

func (c *Cache) PutSuggestion(key, suggestion string) error {
	return c.writeEntry("suggestions", key, suggestion)
}

func (c *Cache) GetQuestions(key string) ([]string, bool, error) {
	var questions []string
	found, err := c.readEntry("questions", key, &questions)

	return questions, found, err
}

func (c *Cache) PutQuestions(key string, questions []string) error {
	return c.writeEntry("questions", key, questions)
}

func (c *Cache) readEntry(namespace, key string, value any) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(c.Dir, namespace, key+".json"))
	if os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if err := json.Unmarshal(raw, value); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Cache) writeEntry(namespace, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return atomicfile.Write(filepath.Join(c.Dir, namespace, key+".json"), encoded)
}
