package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
)

type AnalysisResult struct {
	Violation  bool   `json:"violation"`
	Reasoning  string `json:"reasoning"`
	QuotedCode string `json:"quoted_code"`
}

type DriftInput struct {
	ADRContent  string
	CodeContext string
	Filename    string
}

func AnalyzeDrift(ctx context.Context, chat Chatter, in DriftInput, systemPrompt string) (*AnalysisResult, error) {
	prompt := GetAnalyzeDriftPrompt(in.ADRContent, in.CodeContext, in.Filename)
	return chatJSON[AnalysisResult](ctx, chat, systemPrompt, prompt, "analysis")
}

type suggestionResult struct {
	Suggestion string `json:"suggestion"`
}

// SuggestRemediation should only be called after AnalyzeDrift has returned
// Violation == true.
func SuggestRemediation(ctx context.Context, chat Chatter, in DriftInput, violation AnalysisResult) (string, error) {
	prompt := GetSuggestionPrompt(in.ADRContent, in.CodeContext, in.Filename, violation.Reasoning, violation.QuotedCode)

	result, err := chatJSON[suggestionResult](ctx, chat, SuggestionSystemPrompt, prompt, "suggestion generation")
	if err != nil {
		return "", err
	}

	return result.Suggestion, nil
}

func chatJSON[T any](ctx context.Context, chat Chatter, systemPrompt, userPrompt, operationLabel string) (*T, error) {
	const maxRetries = 3

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 2 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0
	bo.MaxElapsedTime = 0 // no overall deadline; ctx handles cancellation

	var lastErr error
	var final T

	operation := func() error {
		raw, err := chat.Chat(ctx, systemPrompt, userPrompt)
		if err != nil {
			lastErr = err
			return err
		}

		cleaned := CleanJSON(raw)

		var res T
		if err := json.Unmarshal([]byte(cleaned), &res); err != nil {
			if err2 := json.Unmarshal([]byte(raw), &res); err2 != nil {
				lastErr = fmt.Errorf("invalid json from provider: %w", err2)
				return lastErr
			}
		}

		final = res
		return nil
	}

	retryPolicy := backoff.WithContext(backoff.WithMaxRetries(bo, maxRetries), ctx)
	if err := backoff.Retry(operation, retryPolicy); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		return nil, fmt.Errorf("%s failed after %d retries: %w", operationLabel, maxRetries, lastErr)
	}

	return &final, nil
}

func CleanJSON(input string) string {
	input = strings.TrimSpace(input)
	start := strings.Index(input, "{")
	end := strings.LastIndex(input, "}")

	if start != -1 && end != -1 && end > start {
		return input[start : end+1]
	}

	return input
}
