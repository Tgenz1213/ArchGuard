---
title: Configurable topK ADR limit
status: Accepted
scope: "internal/**"
---

# Configurable topK ADR limit

## Context

`Engine.Run` hardcoded the number of ADRs considered per file to a `const topKADRs = 3`, passed to `Store.Search` (non-debug) or `Store.SearchWithDebugInfo` (`--debug`). A file can legitimately be relevant to more than 3 ADRs that all pass both `scope` and `similarity_threshold` -- a shared utility file, a config loader, or any file several architectural rules structurally apply to. `rankAndLimit` (`internal/index/rank.go`) silently truncated the sorted, already-threshold-passing candidate list to 3, dropping every other qualifying ADR before the LLM ever saw it, with no way to raise the limit short of editing Go source and rebuilding. See #189.

## Decision

A new `analysis.max_relevant_adrs` config field (`config.Analysis.MaxRelevantADRs`, plain `int`) replaces the `topKADRs` constant, following the same fallback pattern as `analysis.max_concurrency`: `<= 0` (including unset, the zero value) defaults to 3. Unlike `vector_store.reindex_threshold` etc., this doesn't need the `*int`/nil-default pattern -- a topK of 0 or negative has no legitimate meaning, so there's no ambiguity between "unset" and "explicit zero" to resolve.

`Engine.Run` reads this value once per `Run` call (alongside `concurrency`) and passes it as `topKADRs` to `Store.Search` in the non-debug path and `Store.SearchWithDebugInfo` in the `--debug` path -- both call sites already took `topK` as a parameter (per `docs/arch/0007-scope-filtered-before-topk-similarity.md`), so no interface change was needed in `internal/index`.

## Consequences

- A project whose ADR corpus regularly produces more than 3 relevant, in-threshold ADRs per file can raise `analysis.max_relevant_adrs` in `archguard.yaml` without a custom build.
- `PgStore.Search`'s `MaxSearchCandidates` (1000) internal fetch cap is unaffected and still comfortably exceeds any reasonable configured topK -- a project would need to both configure an extreme topK and have a corpus that large before the two constants interact.
- Raising this limit doesn't eliminate the need for the `--debug` topK-truncation diagnostic (`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md`'s amendment for #190) -- it only moves where the cutoff falls, so a corpus can still exceed a raised limit.
