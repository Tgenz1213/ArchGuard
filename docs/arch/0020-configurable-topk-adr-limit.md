---
title: Configurable topK ADR limit
status: Accepted
scope: "internal/**"
---

# Configurable topK ADR limit

## Context

The number of ADRs considered per file cannot be a fixed 3. A file can legitimately be relevant to more than 3 ADRs that all pass both `scope` and `similarity_threshold` -- a shared utility file, a config loader, or any file several architectural rules structurally apply to. A fixed limit silently drops every other qualifying ADR before the LLM sees it and can't be raised short of editing Go source and rebuilding. See #189.

## Decision

A new `analysis.max_relevant_adrs` config field (`config.Analysis.MaxRelevantADRs`, plain `int`) replaces the `topKADRs` constant, following the same fallback pattern as `analysis.max_concurrency`: `<= 0` (including unset, the zero value) defaults to 3. Unlike `vector_store.reindex_threshold` etc., this doesn't need the `*int`/nil-default pattern -- a topK of 0 or negative has no legitimate meaning, so there's no ambiguity between "unset" and "explicit zero" to resolve.

`config.Analysis.RelevantADRLimit()` resolves the value. `Engine.Run` passes it to `stage.NewCosineStage` when no `analysis.pipeline` is configured, and `analysis.BuildStages` uses it as `rank`'s default `top_k`; either way it becomes the cosine stage's `MaxKeep` (see `docs/arch/0022-candidate-scoring-pipeline.md`).

## Consequences

- A project whose ADR corpus regularly produces more than 3 relevant, in-threshold ADRs per file can raise `analysis.max_relevant_adrs` in `archguard.yaml` without a custom build.
- `PgStore.Search`'s `MaxSearchCandidates` (1000) internal fetch cap is unaffected and still comfortably exceeds any reasonable configured topK -- a project would need to both configure an extreme topK and have a corpus that large before the two constants interact.
- Raising this limit doesn't eliminate the need for the `--debug` topK-truncation diagnostic (`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md`'s amendment for #190) -- it only moves where the cutoff falls, so a corpus can still exceed a raised limit.
