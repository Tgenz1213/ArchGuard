package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

/**
 * REGION: Types & Interfaces
 */

type AnalysisResult struct {
	Violation  bool   `json:"violation"`
	Reasoning  string `json:"reasoning"`
	QuotedCode string `json:"quoted_code"`
}

type Provider interface {
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)
	Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)
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

### TASK
Does the code_context literally violate the 'Decision' section of the ADR?

### LOGICAL STEPS:
1. Identify the literal requirement in the ADR.
2. Identify the actual implementation in the code_context.
3. If they match or don't explicitly contradict, violation is false.

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

func AnalyzeDrift(ctx context.Context, p Provider, adrContent, codeContext, filename, systemPrompt string) (*AnalysisResult, error) {
	prompt := GetAnalyzeDriftPrompt(adrContent, codeContext, filename)

	maxRetries := 3
	backoff := 2 * time.Second
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		raw, err := p.Chat(ctx, systemPrompt, prompt)
		if err != nil {
			lastErr = err
			continue
		}

		cleaned := CleanJSON(raw)
		var res AnalysisResult
		if err := json.Unmarshal([]byte(cleaned), &res); err != nil {
			// Second attempt at unmarshaling raw output
			if err2 := json.Unmarshal([]byte(raw), &res); err2 != nil {
				lastErr = fmt.Errorf("invalid json from provider: %w", err2)
				continue
			}
		}
		return &res, nil
	}

	return nil, fmt.Errorf("analysis failed after %d retries: %w", maxRetries, lastErr)
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
