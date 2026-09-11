---
title: Scope filtered before the topK similarity cut
status: Accepted
scope: "internal/**"
---

# Scope filtered before the topK similarity cut

## Context

`Engine.Run` called `Store.Search(embedding, threshold, 3)` -- a fixed top-3 by cosine similarity -- and only afterward glob-matched each hit's `scope` against the file being analyzed. `PgStore.Search`'s SQL had no `scope` predicate at all; `LocalStore.Search` had the identical shape in Go: rank first, filter second.

Because scope was applied only to whatever survived the top-3 similarity cut, a correctly-scoped ADR with merely lower cosine similarity than three unrelated-scope ADRs never entered the candidate set at all -- its scope match was never evaluated, and nothing was logged, because from `Search`'s perspective there was nothing to suppress; the ADR simply never existed as a candidate. See #134.

## Decision

`VectorStore.Search` gains a `filePath string` parameter and now applies scope as a structural filter *before* ranking and limiting to `topK`, not after. Both backends funnel through two shared helpers in `internal/index/rank.go`:

1. `filterByScope(candidates, filePath)` -- keeps only candidates with no `scope` set, or whose `scope` glob matches `filePath`.
2. `rankAndLimit(candidates, topK)` -- sorts the (already scope-filtered) survivors by descending similarity and cuts to `topK`.

`LocalStore.Search` computes cosine similarity for every in-memory ADR above `threshold`, then runs both helpers -- cheap, since the whole corpus already lives in memory.

`PgStore.Search` cannot push `scope` matching into SQL: scope is a `doublestar` glob (supports `**`), and hand-translating that into Postgres `LIKE`/regex would mean two independent glob-matching implementations that must stay behaviorally identical forever -- a real risk of silent backend divergence for an edge case (`**` crossing path separators) that's easy to get subtly wrong and hard to notice. Two options were considered: translate the glob to SQL (keeps the tight `LIMIT topK` and full HNSW ANN efficiency, at the cost of that duplication risk), or widen the SQL fetch and reuse the exact same Go-side filtering `LocalStore` uses. The second was chosen: `PgStore.Search` now queries with `LIMIT` set to a new constant, `maxSearchCandidates` (1000), instead of the caller's `topK`, fetching every above-threshold row for the project up to that cap, then runs the same `filterByScope`/`rankAndLimit` pair. This guarantees the two backends can never diverge in scope-matching behavior, since there is exactly one implementation of it.

`internal/index` cannot import `internal/analysis` (the reverse already holds, for `index.VectorStore` etc.), so the glob-matching helper itself moved from `internal/analysis` (unexported `matchGlob`) to `internal/index` (exported `MatchGlob`). `internal/analysis` now calls `index.MatchGlob` for its own unrelated `exclude_patterns` check, so there remains exactly one glob matcher in the codebase.

`Engine.Run`'s own post-hoc scope check (in the `for _, hit := range hits` loop) was deleted rather than left in place as a "belt and suspenders" redundancy: `Search` is now the sole source of truth for scope matching, and leaving a second check at the call site would silently mask a future regression in `Search`'s own filtering instead of surfacing it as a test failure where the logic actually lives.

## Consequences

- A scope-matching ADR is now considered as a candidate whenever it exists in the corpus, regardless of how many higher-similarity non-matching-scope ADRs exist -- fixing the original bug for both backends identically.
- `PgStore.Search` now fetches up to `maxSearchCandidates` (1000) rows per query instead of `topK`, trading some ANN efficiency (the HNSW graph walk must satisfy a larger `LIMIT`) for correctness. For realistic ADR corpus sizes (dozens to low hundreds per project) this is a minor, bounded cost; a corpus large enough to exceed 1000 above-threshold ADRs for one project would need this cap revisited (or the SQL-glob-translation alternative reconsidered) -- not expected at this codebase's current scale.
- `BenchmarkPgStoreSearch_ProjectFiltering` (`internal/index/pgvector_bench_test.go`, requires Docker, not run in CI) now exercises this wider internal fetch. Its recall and latency numbers will differ from pre-#134 baselines -- recall should improve (a larger internal candidate pool makes the post-hoc `project_name` filter's recall loss, the subject of #44 and ADR-0005, less likely to bite), latency may increase somewhat. This is an accepted, expected shift, not a regression in the benchmark itself.
- `maxSearchCandidates` is an internal constant, not exposed via `archguard.yaml`. If a real corpus ever needs it tuned, that's a small follow-up (a `*int` config field following the existing nil-default pattern used by `reindex_threshold` etc.), not a reason to block this fix.
