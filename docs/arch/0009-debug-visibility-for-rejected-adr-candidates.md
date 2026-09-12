---
title: Debug visibility for rejected ADR candidates
status: Accepted
scope: "internal/**"
---

# Debug visibility for rejected ADR candidates

## Context

`Store.Search` discards sub-threshold ADRs internally via `filterByThreshold`. `Engine` never sees them, so `--debug`'s only score-logging line fired solely for hits that already passed both the threshold and the top-3 cut. There was no way, even in `--debug`, to tell "this ADR doesn't exist," "it exists but scored too low," and "it wasn't embedded yet" apart -- they all looked identical: silence. See #137.

## Decision

`VectorStore` gains a second method, `SearchRejected(queryEmbedding []float32, threshold float64, topK int, filePath string) []SearchResult`, with the same scope-filter-then-rank shape as `Search` (reusing `filterByScope`/`rankAndLimit` from `internal/index/rank.go`, per `docs/arch/0007-scope-filtered-before-topk-similarity.md`'s precedent), but keeping candidates *below* threshold instead of at-or-above it via a new `filterBelowThreshold` helper -- `filterByThreshold`'s exact complement, run in the same pipeline position.

`LocalStore.SearchRejected` is a straightforward second pass over the in-memory ADRs: enumerate, `filterByScope`, `filterBelowThreshold`, `rankAndLimit` -- identical shape to `LocalStore.Search`. `PgStore.SearchRejected` reuses the same `SearchQuery` `Search` does (already threshold-less as of #140/#141's fix, bounded by `MaxSearchCandidates`), then applies `filterByScope`, `filterBelowThreshold`, `rankAndLimit` in Go. The row-scanning loop shared by both queries was factored into `scanSearchResults` to avoid duplicating it between `Search` and `SearchRejected`.

`Engine.Run` calls `SearchRejected` only inside `if e.Debug`, immediately after the existing `Search` call, logging each rejected candidate's title and score. Because the call site is debug-gated, non-debug behavior and cost are unchanged by construction -- no new query runs, nothing new is printed, outside `--debug`.

An alternative considered: have `Search` itself always compute and return rejected candidates (e.g. as a second return value), letting `Engine` decide whether to print them. This was rejected because it would mean `LocalStore.Search` and `PgStore.Search` do strictly more work on every call, including the non-debug path that is the overwhelming majority of real usage -- violating the "non-debug performance unchanged" requirement. A separate, debug-only method keeps the hot path untouched.

## Consequences

- `--debug` output now shows, per file, which scope-matched ADRs were excluded for scoring below `similarity_threshold`, with their title and score -- closing the diagnostic gap from #137 for the threshold-miss case (the scope-miss case was already closed by #134 / ADR-0007's scope-filter-before-rank fix, which also lets scope-matched near-misses be found at all).
- `PgStore.SearchRejected` issues a second query when invoked, structurally identical in cost to `Search` itself (bounded by `MaxSearchCandidates`). This only happens in `--debug` mode, so it does not affect steady-state `check --ci` cost.
- Both backends must keep `SearchRejected` behaviorally aligned with `Search`'s scope-filtering the same way `docs/arch/0007` already requires for the accepted-candidate path, and now also with `Search`'s threshold-filtering ordering per #140/#141. `filterByScope` and `rankAndLimit` are literally shared, so a change there can't diverge. `filterByThreshold` and `filterBelowThreshold` are separate functions (one keeps `>= threshold`, the other its complement) rather than one shared filter, so a naive future edit to one without the other could drift them apart -- to close that gap, both key off a single `meetsThreshold` predicate, so a change to threshold *semantics* (e.g. a per-ADR override) only needs to happen in one place and both filters stay exact complements automatically.
