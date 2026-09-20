package llm

import (
	"fmt"
	"strings"
)

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

// EscapePromptDelimiter neutralises the prompt's container delimiters to block prompt injection.
func EscapePromptDelimiter(input string) string {
	s := strings.ReplaceAll(input, "</adr_content>", "[ADR_END]")
	s = strings.ReplaceAll(s, "</code_context>", "[CODE_END]")
	return strings.ReplaceAll(s, "```", "'''")
}

// sanitizeFilename also strips line breaks: the filename sits on its own unquoted
// "File Path:" line, not inside a delimited block.
func sanitizeFilename(filename string) string {
	s := EscapePromptDelimiter(filename)
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

func GetAnalyzeDriftPrompt(adrContent, codeContext, filename string) string {
	safeADR := EscapePromptDelimiter(adrContent)
	safeCode := EscapePromptDelimiter(codeContext)
	safeFilename := sanitizeFilename(filename)

	return fmt.Sprintf(ChatPrompt, safeFilename, safeADR, safeCode)
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
	safeFilename := sanitizeFilename(filename)

	return fmt.Sprintf(SuggestionPrompt, safeFilename, safeADR, safeCode, safeReasoning, safeQuoted)
}
