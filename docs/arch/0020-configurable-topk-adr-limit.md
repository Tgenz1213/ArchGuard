---
title: Configurable topK ADR limit
status: Accepted
scope: "internal/**"
---

# Configurable topK ADR limit

## Context

The number of ADRs considered per file was a fixed 3. A file can legitimately be relevant to more ADRs that all pass scope and similarity threshold: a shared utility, a config loader, or any file several architectural rules apply to. A fixed limit silently drops every other qualifying ADR before the model sees it, and raising it meant editing Go source and rebuilding. See #189.

## Decision

The limit is the `analysis.max_relevant_adrs` setting. Like `analysis.max_concurrency`, an unset or non-positive value falls back to the default, so it stays a plain `int` (see the zero-value rule in `docs/standards/paradigm.md`). It becomes the cosine stage's keep limit, and the default `top_k` of a configured `rank` stage (`docs/arch/0022-candidate-scoring-pipeline.md`).

## Consequences

- A project whose files regularly match more relevant ADRs than the default can raise the limit in `archguard.yaml` without a custom build.
- The Postgres store's internal candidate cap stays far above any reasonable limit, so the two interact only with an extreme limit and a corpus of that size.
- Raising the limit does not remove the need for `--debug` to show the ADRs the limit cut. It only moves the cutoff, and a corpus can still exceed a raised limit.
