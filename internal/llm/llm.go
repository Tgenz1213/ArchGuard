package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
)

/**
 * REGION: Types & Interfaces
 */

type AnalysisResult struct {
	Violation  bool   `json:"violation"`
	Reasoning  string `json:"reasoning"`
	QuotedCode string `json:"quoted_code"`
	// Suggestion is populated only when Violation is true, via a separate
	// call (SuggestRemediation) gated by the caller -- never by this prompt.
	Suggestion string `json:"suggestion,omitempty"`
}

// EmbeddingTaskType distinguishes why an embedding is being created, so an
// asymmetric-retrieval-capable provider can tune the vector for that role.
type EmbeddingTaskType int

const (
	// EmbeddingTaskDocument marks content being indexed into the vector
	// store (ADR content).
	EmbeddingTaskDocument EmbeddingTaskType = iota
	// EmbeddingTaskQuery marks content being embedded to search the
	// vector store (diff/code content).
	EmbeddingTaskQuery
)

// Pick returns document or query depending on t, so providers can map the
// task role onto their own backend's convention in one line.
func (t EmbeddingTaskType) Pick(document, query string) string {
	if t == EmbeddingTaskQuery {
		return query
	}
	return document
}

type Provider interface {
	// CreateEmbedding embeds text for the given task role. Providers
	// without an asymmetric-retrieval mechanism may ignore task.
	CreateEmbedding(ctx context.Context, text string, task EmbeddingTaskType) ([]float32, error)
	Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// CountTokens uses each provider's own tokenizer, not a shared one.
	CountTokens(ctx context.Context, text string) (int, error)
}

/**
 * REGION: Prompts
 */

const DefaultSystemPrompt = `You are a literal-minded Architectural Compliance Auditor.
Your ONLY task is to identify direct contradictions between the provided Code and the mandatory 'Decision' section of the ADR.

CRITICAL GUIDELINES:
1. COMPLIANCE IS NOT A VIOLATION: If the code follows the rule (e.g. ADR says "Use Go" and code is Go), it is NOT a violation.
2. NO INFERENCE: Do not assume "intent." If the ADR says "Use Go" and the code is Go, it is a PASS.
3. NO STYLE NITS: Do not flag unidiomatic code unless the ADR explicitly forbids it.
4. FALSE BY DEFAULT: If you cannot find a clear, literal contradiction, "violation" MUST be false.`

const ChatPrompt = `### INPUT DATA
File Path: %s

<adr_content>
%s
</adr_content>

<code_context>
%s
</code_context>

### OUTPUT FORMAT (JSON ONLY)
{
  "violation": bool,
  "reasoning": "Single sentence explaining the contradiction.",
  "quoted_code": "The snippet breaking the rule."
}`

// EscapePromptDelimiter prevents prompt injection by neutralising common LLM delimiters.
func EscapePromptDelimiter(input string) string {
	// Neutralize XML tags and triple backticks to prevent escaping the prompt containers
	s := strings.ReplaceAll(input, "</adr_content>", "[ADR_END]")
	s = strings.ReplaceAll(s, "</code_context>", "[CODE_END]")
	return strings.ReplaceAll(s, "```", "'''")
}

func GetAnalyzeDriftPrompt(adrContent, codeContext, filename string) string {
	// Sanitize inputs before formatting into the template
	safeADR := EscapePromptDelimiter(adrContent)
	safeCode := EscapePromptDelimiter(codeContext)

	return fmt.Sprintf(ChatPrompt, filename, safeADR, safeCode)
}

const SuggestionSystemPrompt = `You are an Architectural Remediation Advisor.
An Architectural Compliance Auditor has already confirmed a real violation between the provided Code and the ADR's 'Decision' section. Your ONLY task is to suggest a short, actionable remediation pointer for a human to follow.

CRITICAL GUIDELINES:
1. NOT A PATCH: Describe the change in prose. Do not write a code diff or claim the fix is complete or verified.
2. BE SPECIFIC: Reference the ADR's actual rule, not generic advice.
3. BE BRIEF: One or two sentences.`

const SuggestionPrompt = `### INPUT DATA
File Path: %s

<adr_content>
%s
</adr_content>

<code_context>
%s
</code_context>

### CONFIRMED VIOLATION
Reasoning: %s
Quoted Code: %s

### OUTPUT FORMAT (JSON ONLY)
{
  "suggestion": "A short remediation pointer, one or two sentences. Not a code patch."
}`

func GetSuggestionPrompt(adrContent, codeContext, filename, reasoning, quotedCode string) string {
	safeADR := EscapePromptDelimiter(adrContent)
	safeCode := EscapePromptDelimiter(codeContext)
	safeReasoning := EscapePromptDelimiter(reasoning)
	safeQuoted := EscapePromptDelimiter(quotedCode)

	return fmt.Sprintf(SuggestionPrompt, filename, safeADR, safeCode, safeReasoning, safeQuoted)
}

func AnalyzeDrift(ctx context.Context, p Provider, adrContent, codeContext, filename, systemPrompt string) (*AnalysisResult, error) {
	prompt := GetAnalyzeDriftPrompt(adrContent, codeContext, filename)
	return chatJSON[AnalysisResult](ctx, p, systemPrompt, prompt, "analysis")
}

type suggestionResult struct {
	Suggestion string `json:"suggestion"`
}

// SuggestRemediation should only be called after AnalyzeDrift has returned
// Violation == true.
func SuggestRemediation(ctx context.Context, p Provider, adrContent, codeContext, filename, reasoning, quotedCode string) (string, error) {
	prompt := GetSuggestionPrompt(adrContent, codeContext, filename, reasoning, quotedCode)
	result, err := chatJSON[suggestionResult](ctx, p, SuggestionSystemPrompt, prompt, "suggestion generation")
	if err != nil {
		return "", err
	}
	return result.Suggestion, nil
}

func chatJSON[T any](ctx context.Context, p Provider, systemPrompt, userPrompt, operationLabel string) (*T, error) {
	const maxRetries = 3

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 2 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0
	bo.MaxElapsedTime = 0 // no overall deadline; ctx handles cancellation

	var lastErr error
	var final T

	operation := func() error {
		raw, err := p.Chat(ctx, systemPrompt, userPrompt)
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
