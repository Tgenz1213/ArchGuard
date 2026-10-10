package inference

import (
	"fmt"
	"strings"
)

const DefaultSystemPrompt = `You are a literal-minded Architectural Compliance Auditor.
Your ONLY task is to identify direct contradictions between the provided Code and the mandatory 'Decision' section of the ADR.

HOW TO WORK:
1. List each rule the Decision states, including rules phrased in general terms ("all", "every", "never", "must").
2. Check each rule against every line of the code. A rule phrased in general terms covers every case it names: code that does not follow it contradicts it, even when the ADR does not mention that exact construct.
3. Reach a verdict only after every rule has been checked.

CRITICAL GUIDELINES:
1. COMPLIANCE IS NOT A VIOLATION: If the code follows the rule (e.g. ADR says "Use Go" and code is Go), it is NOT a violation.
2. NO INFERENCE: Do not assume "intent." Do not invent rules the Decision does not state.
3. NO STYLE NITS: Do not flag unidiomatic code unless the ADR explicitly forbids it.
4. FALSE BY DEFAULT: If you cannot point to a specific line that contradicts a specific stated rule, "violation" MUST be false.`

const ChatPrompt = `### INPUT DATA
File Path: %s

<adr_content>
%s
</adr_content>

<code_context>
%s
</code_context>

### OUTPUT FORMAT (JSON ONLY, fields in this order)
{
  "reasoning": "One or two sentences: the rule you checked, the line you checked it against, and whether the line follows or contradicts it.",
  "quoted_code": "The snippet breaking the rule, or an empty string when there is none.",
  "violation": bool
}`

const ChatPromptDiff = `### INPUT DATA
File Path: %s

<adr_content>
%s
</adr_content>

<code_context>
%s
</code_context>

### HOW TO READ THE CODE
The code context is a unified diff of one file. Lines starting with "+" were added by this change, lines starting with "-" were removed by it, and lines starting with a space are unchanged context.
Check every added line against each rule in the ADR's Decision, and for every removed line ask whether the ADR requires what it did. Judge the code this change leaves behind. Report a violation only when (a) an added line directly contradicts the ADR, or (b) a removed line was something the ADR requires, so removing it makes the code contradict the ADR. Removing code that itself broke the ADR is not a violation. Unchanged context lines are never violations.
In "quoted_code", quote the added or removed line without its leading "+" or "-". Several consecutive lines may be quoted together, one per line.

### OUTPUT FORMAT (JSON ONLY, fields in this order)
{
  "reasoning": "One or two sentences: the rule you checked, the line you checked it against, and whether the line follows or contradicts it.",
  "quoted_code": "The added or removed snippet breaking the rule, or an empty string when there is none.",
  "violation": bool
}`

func PromptTemplate(diff bool) string {
	if diff {
		return ChatPromptDiff
	}

	return ChatPrompt
}

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
	return renderDriftPrompt(ChatPrompt, adrContent, codeContext, filename)
}

func GetAnalyzeDriftDiffPrompt(adrContent, codeContext, filename string) string {
	return renderDriftPrompt(ChatPromptDiff, adrContent, codeContext, filename)
}

func renderDriftPrompt(template, adrContent, codeContext, filename string) string {
	return fmt.Sprintf(template, sanitizeFilename(filename), EscapePromptDelimiter(adrContent), EscapePromptDelimiter(codeContext))
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
