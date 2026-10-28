package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	Diff        bool
}

// retries counts the requests sent after the first, whether or not the call succeeded.
func AnalyzeDrift(ctx context.Context, chat Chatter, in DriftInput, systemPrompt string) (result *AnalysisResult, retries int, err error) {
	return chatJSON[AnalysisResult](ctx, chat, systemPrompt, DriftPrompt(in), "analysis")
}

func DriftPrompt(in DriftInput) string {
	return renderDriftPrompt(PromptTemplate(in.Diff), in.ADRContent, in.CodeContext, in.Filename)
}

type suggestionResult struct {
	Suggestion string `json:"suggestion"`
}

type questionResult struct {
	Questions []string `json:"questions"`
}

// GenerateQuestions asks the chat model for yes/no screening questions about an ADR, each phrased so yes means a violation.
func GenerateQuestions(ctx context.Context, chat Chatter, adrContent string) (questions []string, retries int, err error) {
	result, retries, err := chatJSON[questionResult](ctx, chat, QuestionSystemPrompt, GetQuestionPrompt(adrContent), "question generation")
	if err != nil {
		return nil, retries, err
	}

	for _, question := range result.Questions {
		if question = strings.TrimSpace(question); question != "" {
			questions = append(questions, question)
		}
	}

	return questions, retries, nil
}

// SuggestRemediation should only be called after AnalyzeDrift has returned
// Violation == true.
func SuggestRemediation(ctx context.Context, chat Chatter, in DriftInput, violation AnalysisResult) (suggestion string, retries int, err error) {
	prompt := GetSuggestionPrompt(in.ADRContent, in.CodeContext, in.Filename, violation.Reasoning, violation.QuotedCode)

	result, retries, err := chatJSON[suggestionResult](ctx, chat, SuggestionSystemPrompt, prompt, "suggestion generation")
	if err != nil {
		return "", retries, err
	}

	return result.Suggestion, retries, nil
}

func chatJSON[T any](ctx context.Context, chat Chatter, systemPrompt, userPrompt, operationLabel string) (*T, int, error) {
	const maxRetries = 3

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 2 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0
	bo.MaxElapsedTime = 0 // no overall deadline; ctx handles cancellation

	var lastErr error
	var final T

	attempts := 0

	operation := func() error {
		attempts++

		reply, err := chat.Chat(ctx, systemPrompt, userPrompt)
		if err != nil {
			lastErr = err
			return err
		}

		raw := reply.Text

		cleaned := escapeStrayBackslashes(CleanJSON(raw))

		var parsed T
		if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
			if err2 := json.Unmarshal([]byte(raw), &parsed); err2 != nil {
				lastErr = fmt.Errorf("invalid json from provider: %w", err2)
				return lastErr
			}
		}

		final = parsed
		return nil
	}

	retryPolicy := backoff.WithContext(backoff.WithMaxRetries(bo, maxRetries), ctx)
	err := backoff.Retry(operation, retryPolicy)
	retries := attempts - 1

	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, retries, ctxErr
		}

		return nil, retries, fmt.Errorf("%s failed after %d retries: %w", operationLabel, retries, lastErr)
	}

	return &final, retries, nil
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

func escapeStrayBackslashes(input string) string {
	var out strings.Builder
	out.Grow(len(input))

	for i := 0; i < len(input); i++ {
		if input[i] != '\\' {
			out.WriteByte(input[i])
			continue
		}

		if startsJSONEscape(input[i+1:]) {
			out.WriteString(input[i : i+2])
			i++

			continue
		}

		out.WriteString(`\\`)
	}

	return out.String()
}

func startsJSONEscape(rest string) bool {
	if rest == "" {
		return false
	}

	if strings.IndexByte(`"\/bfnrt`, rest[0]) >= 0 {
		return true
	}

	if rest[0] != 'u' || len(rest) < 5 {
		return false
	}

	_, err := strconv.ParseUint(rest[1:5], 16, 16)

	return err == nil
}
