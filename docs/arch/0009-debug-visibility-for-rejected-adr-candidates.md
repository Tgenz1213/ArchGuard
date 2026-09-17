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

## Amendment: topK-truncated candidates (#190)

The same blind spot exists for ADRs that pass both `scope` and
`similarity_threshold` but are still dropped by `rankAndLimit`'s topK cutoff
(`internal/index/rank.go`) -- previously indistinguishable from "no other ADR
was relevant." `VectorStore.SearchTruncated` mirrors `SearchRejected`'s shape and
cost contract (candidates + score, called only inside `if e.Debug`), but
inverts a different pipeline stage: `SearchRejected` complements the
threshold filter (`filterBelowThreshold` instead of `filterByThreshold`)
while ranking normally, whereas `SearchTruncated` filters by threshold
normally and complements `rankAndLimit` instead, via `truncatedByTopK`. In
`--debug` mode, `Engine.Run` logs each one as:

```
  Cut by top-K limit: <title> (score X.XX, rank Y of Z qualifying ADRs)
```

distinct from the existing `"  Below threshold: ..."` line. Non-debug
behavior and cost are unaffected, same as the original decision above.
`SearchTruncated`'s result is intentionally uncapped (unlike `SearchRejected`,
which still ends in `rankAndLimit`) -- every qualifying ADR beyond topK is
reported, not just the closest few. (Superseded by the amendment below:
`PgStore` no longer issues three independent queries for this.)

## Amendment: consolidating hits/rejected/truncated into one query (#192)

`Search`, `SearchRejected`, and `SearchTruncated` each issued their own
independent query. For `PgStore` under `hnsw.iterative_scan = 'relaxed_order'`
(`docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md`), two
separate approximate HNSW queries against the same index and query vector
are not guaranteed to return identical candidate sets -- so within a single
`--debug` run, an ADR could be misreported as both a hit and truncated (or
neither), since the diagnostics were computed from a different query than
the one that produced the actual analyzed hits. Flagged by automated review
on #192.

`VectorStore` gains `SearchWithDebugInfo(queryEmbedding []float32, threshold
float64, topK int, filePath string) (hits, rejected, truncated
[]SearchResult)`, which issues exactly one query, applies `filterByScope`
once, then derives all three outputs from that single candidate set (a
distinct copy per output, since `filterByThreshold`/`filterBelowThreshold`
filter in place): `rejected` from `filterBelowThreshold` + `rankAndLimit`,
`hits` and `truncated` from `filterByThreshold` followed by `rankAndLimit`
and `truncatedByTopK` respectively. `Engine.Run` calls this instead of
`Search`+`SearchRejected`+`SearchTruncated` whenever `e.Debug` is set;
outside `--debug` it still calls plain `Search` alone, so non-debug behavior
and cost are unaffected.

`SearchRejected` and `SearchTruncated` are kept as standalone methods (still
tested, still part of the interface) since existing tests and call sites use
them independently, but `Engine.Run`'s debug path no longer calls them
directly -- `SearchWithDebugInfo` is the only debug-diagnostic entry point
that also needs `hits` to stay consistent with what it prints.
