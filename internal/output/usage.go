package output

import (
	"fmt"
	"strings"
)

// A nil count is one the provider did not report, or under --dry-run an output count that can't be projected.
// A nil cost has no price set or no count to price; under --dry-run it prices the input alone.
type ChatUsage struct {
	InputTokens  *int     `json:"input_tokens"`
	OutputTokens *int     `json:"output_tokens"`
	Cost         *float64 `json:"estimated_cost"`
}

type EmbeddingUsage struct {
	Tokens *int     `json:"tokens"`
	Cost   *float64 `json:"estimated_cost"`
}

// Suggestion is nil when suggestions were off.
type Usage struct {
	Judgment   ChatUsage      `json:"judgment"`
	Suggestion *ChatUsage     `json:"suggestion,omitempty"`
	Embedding  EmbeddingUsage `json:"embedding"`
}

func (u Usage) line() string {
	parts := []string{"judgment " + u.Judgment.describe()}
	if u.Suggestion != nil {
		parts = append(parts, "suggestions "+u.Suggestion.describe())
	}

	parts = append(parts, "embeddings "+u.Embedding.describe())

	return "Tokens: " + strings.Join(parts, "; ") + "."
}

func (c ChatUsage) describe() string {
	switch {
	case c.InputTokens == nil:
		return "unavailable"
	case c.OutputTokens == nil:
		return fmt.Sprintf("%d input, output not projected", *c.InputTokens) + costSuffix(c.Cost)
	default:
		return fmt.Sprintf("%d input, %d output", *c.InputTokens, *c.OutputTokens) + costSuffix(c.Cost)
	}
}

func (e EmbeddingUsage) describe() string {
	if e.Tokens == nil {
		return "unavailable"
	}

	return fmt.Sprintf("%d", *e.Tokens) + costSuffix(e.Cost)
}

func costSuffix(cost *float64) string {
	if cost == nil {
		return ""
	}

	return fmt.Sprintf(" (estimated cost %.6f)", *cost)
}
