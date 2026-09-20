---
title: "LLM-suggested remediation is a second, opt-in call"
status: "Accepted"
scope: "internal/**"
---

# LLM-suggested remediation is a second, opt-in call

## Context

Issue #73 asked for a short remediation pointer alongside each violation report, since knowing *what* and *why* doesn't tell the reader *what to do*. Two designs were considered: fold "and suggest a fix" into the existing violation-judgment prompt (one call, cheaper), or ask for it via a second call made only after a violation is already confirmed.

`DefaultSystemPrompt` (`internal/llm/prompts.go`) is deliberately narrow and "FALSE BY DEFAULT" -- it exists specifically to avoid false positives. Adding "and suggest a fix" to that same prompt risks subtly biasing the model toward finding violations, just to have something to suggest.

Because a suggestion is inherently unverifiable -- ArchGuard has no way to confirm that following it actually resolves the violation -- and because it costs a full extra LLM call per violation, the feature should never run unless a user or CI job explicitly opts in.

## Decision

- `SuggestRemediation` (`internal/llm/analysis.go`) is a separate function with its own prompt (`SuggestionSystemPrompt`/`SuggestionPrompt`), called only from `internal/analysis.Engine.Run` after `AnalyzeDrift` has already returned `Violation: true`, and only for a *newly reported* violation (not `--update-baseline`, not an already-baselined/suppressed one).
- The feature is off by default and gated by the `check --suggest-fixes` flag (`Engine.SuggestFixes`), so no extra LLM cost is incurred unless requested.
- `Violation.Suggestion` is `omitempty` and populated only when a violation is confirmed and `--suggest-fixes` is set. **Amended by `docs/arch/0017-suggestion-cache-key-namespace.md`:** `AnalysisResult` no longer carries a `Suggestion` field at all -- suggestions live only in their own cache namespace, never merged into the judgment result.
- Once computed, the suggestion is cached so a later run of the same file/ADR pair reuses it without another LLM call. **Amended by `docs/arch/0017-suggestion-cache-key-namespace.md`:** this is no longer merged into the judgment `AnalysisResult` cache entry -- it lives under its own key, computed from the suggestion prompt content, so a suggestion-prompt change invalidates only suggestion entries.
- Output is explicitly labeled unverified in both the CLI text (`Suggestion (unverified): ...`) and is documented as an LLM-generated pointer, never a guaranteed fix.

## Consequences

- Positive: no behavior or cost change for anyone not passing `--suggest-fixes`; the violation-judgment prompt's accuracy is untouched since its prompt text never changed.
- Positive: suggestion cost is paid at most once per cache key (model, ADR content, file content, reasoning, quoted code, suggestion prompt -- see `docs/arch/0017-suggestion-cache-key-namespace.md`), even across `--suggest-fixes` on/off toggling.
- Negative: a user who wants suggestions for violations already cached *without* one from a prior non-`--suggest-fixes` run pays exactly one extra call the first time they add the flag -- expected and cheap, not a bug.
- Negative: this is a second round trip per violation when the flag is on, roughly doubling check latency for a file with many violations; acceptable since it's opt-in.
