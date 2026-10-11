---
title: Scope filtered before the topK similarity cut
status: Accepted
scope: "internal/**"
---

# Scope filtered before the topK similarity cut

## Context

The engine asked the vector store for a fixed top 3 ADRs by cosine similarity and only then glob-matched each hit's `scope` against the file. Both backends ranked first and filtered second.

A correctly scoped ADR with lower similarity than three ADRs of unrelated scope never became a candidate. Its scope was never evaluated and nothing was logged, because from the store's side there was nothing to suppress. See #134.

## Decision

`VectorStore.Search` takes the file path and applies scope as a filter before ranking and cutting to `topK`. Both backends share one Go implementation of the filter-then-rank steps.

The Postgres store does not push scope matching into SQL. It widens the SQL fetch and reuses the Go-side filtering the local store uses. The Postgres query fetches the nearest rows for the project up to the `MaxSearchCandidates` cap, then runs the shared filters. With one implementation, the backends cannot diverge on scope.

`internal/index` cannot import `internal/analysis`, so the glob matcher lives in `internal/index` and is exported. `internal/analysis` uses it for `exclude_patterns`, so the codebase has exactly one glob matcher.

The engine's own post-hoc scope check is removed. Search is the single source of truth for scope.

The similarity threshold follows the same rule (#140). Threshold filtering runs after scope filtering and before the `topK` cut, in Go, in both backends. SQL carries no threshold predicate. The reasoning matches scope: a per-ADR threshold can only be applied once the candidate's identity, and so its scope, is known.

## Rules

- `VectorStore.Search` MUST apply scope as a filter before ranking and cutting to `topK`.
- Both backends MUST share one Go implementation of the filter-then-rank steps.
- The Postgres store MUST NOT push scope matching into SQL.
- The codebase MUST have exactly one glob matcher, the exported one in `internal/index`.
- The engine MUST NOT re-check scope on Search results.
- Threshold filtering MUST run after scope filtering and before the `topK` cut, in Go, in both backends, and SQL MUST NOT carry a threshold predicate.

## Alternatives considered

- **Translate the glob to SQL.** This keeps the tight `LIMIT topK` and full HNSW efficiency. But scope is a `doublestar` glob, and translating it to `LIKE` or a regex would create a second glob matcher that must stay identical to the first forever. Edge cases such as `**` crossing path separators are easy to get subtly wrong and hard to notice.
- **Keep the engine's post-hoc scope check as a redundant safeguard.** A second check at the call site would mask a future regression in Search instead of surfacing it as a test failure where the logic lives.
- **Apply the similarity threshold before scope filtering, in SQL or at collection time.** This repeated the original bug.

## Consequences

- A scope-matching ADR is a candidate whenever it is in the corpus, however many higher-similarity ADRs of other scopes exist. Both backends behave identically.
- The Postgres store fetches up to `MaxSearchCandidates` rows per query instead of `topK`, trading some ANN efficiency for correctness. For realistic corpora of dozens to low hundreds of ADRs per project the cost is small and bounded. A project whose corpus exceeds the cap would need the cap revisited, or the SQL-translation alternative reconsidered.
- That guarantee holds only while HNSW iterative scan is active (`docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md`). The codebase never sets `hnsw.ef_search`, so with iterative scan off or on an older pgvector, an HNSW scan returns at most `ef_search` candidates regardless of `LIMIT`. #134 can then resurface in weaker form for a scope-matching ADR ranked below that point.
- Without a threshold predicate, a query typically returns the full per-project set up to the cap rather than the above-threshold subset. That is a bounded increase in bytes per query. In exchange, the weaker `WHERE` clause lets the HNSW scan reach the `LIMIT` after examining fewer index tuples.
- The Postgres project-filtering benchmark exercises the wider fetch, so its recall and latency numbers shift from earlier baselines. Because Search re-sorts a wide fetch in Go before returning its top 3, the returned results often match the exact oracle even when the underlying HNSW project-filtered recall (#44, ADR-0005) is poor. The benchmark is a weaker instrument for that problem. A future probe would need to measure recall at the SQL/HNSW layer directly.
- `MaxSearchCandidates` is an internal constant, not a config key. If a real corpus needs it tuned, a config field is a small follow-up.
