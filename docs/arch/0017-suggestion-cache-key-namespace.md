---
title: "Suggestion cache lives in its own key namespace, separate from judgment"
status: "Accepted"
scope: "internal/cache/**"
---

# Suggestion cache lives in its own key namespace, separate from judgment

## Context

`docs/arch/0016-llm-suggested-remediation.md` first merged a computed suggestion into the cached judgment result, under the judgment cache key. That key does not include the suggestion prompt, so an edit to the suggestion prompt kept serving the stale, pre-edit suggestion for every file and ADR already cached. A GitHub Copilot review on PR #165 flagged this, tracked as issue #167.

## Decision

- Suggestions are cached under their own content-addressed key, separate from the judgment key. It covers the model, the ADR content, the file content, the filename, the judgment's reasoning and quoted code, and the suggestion system prompt and prompt template. The filename is included because the rendered suggestion prompt contains the file path (#167). The key inputs are named struct fields, not positional strings, so same-typed inputs cannot be transposed at the call site (#183).
- Suggestion entries are stored apart from judgment entries, never inside the judgment result.
- The judgment result does not carry the suggestion. Suggestions flow only through their own cache.
- Because the key is content-addressed, not versioned, a suggestion-prompt change produces a new key. The old entry becomes an unreachable miss, with no invalidation step, version bump or cache wipe.

## Rules

- Suggestions MUST be cached under their own content-addressed key, separate from the judgment key.
- The suggestion key MUST cover the model, the ADR content, the file content, the filename, the judgment's reasoning and quoted code, and the suggestion system prompt and prompt template.
- The suggestion key inputs MUST be named struct fields, not positional strings.
- Suggestion entries MUST be stored apart from judgment entries, and the judgment result MUST NOT carry the suggestion.

## Alternatives considered

- **Add the suggestion prompt to the shared judgment key.** It would invalidate judgment entries whenever only the suggestion prompt changes, coupling two prompts that evolve independently for no benefit to judgment.

## Consequences

- Positive: editing the suggestion prompt regenerates suggestions for cached violations, without touching judgment entries or needing a manual wipe of `.archguard/cache/`.
- Positive: an unrelated engine setting, such as `--debug`, still reuses a cached suggestion, since it is in neither key.
- Negative: a suggestion-prompt edit leaves the old entries as orphan files. The cache has no TTL or garbage collection, so they stay. This is acceptable at this cache's size, and is the same trade-off already accepted when the embedding computation changes.
