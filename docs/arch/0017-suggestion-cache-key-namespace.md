---
title: "Suggestion cache lives in its own key namespace, separate from judgment"
status: "Accepted"
scope: "internal/cache/**"
---

# Suggestion cache lives in its own key namespace, separate from judgment

## Context

`docs/arch/0016-llm-suggested-remediation.md` originally had `Engine.Run` merge a computed suggestion back into the cached `AnalysisResult` and re-persist it under `cache.ComputeAnalysisKey`'s existing key (model, ADR content, file content, judgment system prompt, judgment prompt template). That key never included `llm.SuggestionSystemPrompt`/`llm.SuggestionPrompt`, so a future edit to the suggestion prompt text would silently keep serving a stale, pre-edit suggestion for any file/ADR pair already cached -- `--suggest-fixes` would never notice the prompt changed. This was flagged by a GitHub Copilot review on PR #165 and tracked as issue #167.

Folding the suggestion prompt into the shared judgment key was rejected: it would invalidate judgment cache entries too whenever only the suggestion prompt changes, coupling two independently-evolving prompts for no benefit to the judgment side.

## Decision

- Suggestions are cached under a distinct, content-addressed key computed by `cache.ComputeSuggestionKey(modelName, adrContent, fileContent, reasoning, quotedCode, suggestionSystemPrompt, suggestionPromptTemplate)` -- separate from `cache.ComputeAnalysisKey`.
- Suggestion entries are stored via new `Cache.GetSuggestion`/`Cache.PutSuggestion` methods, under `.archguard/cache/suggestions/`, never inside the judgment `AnalysisResult` JSON file.
- `AnalysisResult.Suggestion` is no longer written to by `Engine.Run`; suggestions flow through the separate cache only.
- Because the key is content-addressed (not versioned), a suggestion-prompt change automatically produces a new key -- a suggestion cached under the old prompt simply becomes an unreachable cache miss under the new key, with no explicit invalidation step, version bump, or cache wipe required.

## Consequences

- Positive: editing `SuggestionSystemPrompt`/`SuggestionPrompt` now correctly triggers regeneration for previously-cached violations, without touching judgment cache entries or requiring a manual `.archguard/cache/` wipe.
- Positive: an unrelated engine setting change (e.g. `--debug`) still reuses a cached suggestion, since it was never part of either key.
- Negative: a suggestion prompt edit leaves the old suggestion entries as permanent orphan files under `.archguard/cache/suggestions/` (no TTL or GC, consistent with the rest of `internal/cache`) -- acceptable at this codebase's cache size, same tradeoff already accepted for the embeddings-computation-change footgun.
