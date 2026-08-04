---
title: HNSW iterative scan for project-filtered search
status: Accepted
scope: "internal/**"
---

# HNSW iterative scan for project-filtered search

## Context

`PgStore.Search` filters a single shared HNSW index (`archguard_adrs_embedding_idx`) by `project_name` after the ANN graph walk, not before -- every project's ADRs live in the same `archguard_adrs` table and the same index. Benchmark data gathered for #44 (implemented in #63 as `BenchmarkPgStoreSearch_ProjectFiltering`) shows this causes real, scale-dependent recall loss as tenant count grows, because HNSW's graph walk stops once it has found `topK` candidates -- regardless of how many of them survive the `project_name` filter:

| Scale | Baseline recall |
|---|---|
| 1 project | 100% |
| 10 projects, 25 ADRs | 88% |
| 10 projects, 100 ADRs | 72.7% |
| 50 projects, 25 ADRs | 28% |
| 50 projects, 100 ADRs | 23.3% |

At 50 projects sharing the table, the default configuration returns fewer than one relevant ADR per query on average (`results_per_query` 0.82-0.84 out of 3 requested). This degrades silently: `Engine.Run` has no signal that a `Search` call returned an incomplete result set, so a real architectural drift can go unflagged with no error or warning, purely because other tenants' ADRs happen to share the index.

## Decision

`PgStore` sets pgvector's `hnsw.iterative_scan = 'relaxed_order'` GUC on every pooled connection via `pgxpool.Config.AfterConnect`, right after `pgxvec.RegisterTypes`. This makes the HNSW graph walk continue past the point where it has `topK` raw candidates, resuming the scan until it has `topK` candidates that also satisfy the `project_name` filter (up to pgvector's own internal scan-size cap), recovering most of the recall lost to post-hoc filtering.

`hnsw.iterative_scan` was only added to pgvector in 0.8.0, so `NewPgStore` runtime-checks the installed version before setting it: `SELECT extversion FROM pg_extension WHERE extname = 'vector'`, checked against `IterativeScanSupportedVersion` (major > 0, or major == 0 and minor >= 8), on the same `tempConn` already used to run `CREATE EXTENSION IF NOT EXISTS vector`. This check is required, not optional, because Postgres does not error on `SET` of an unrecognized GUC name -- confirmed during #63's benchmark development, this is exactly what made an earlier `SET`-and-catch-the-error approach unsound: a pre-0.8.0 installation would silently accept the `SET` and silently keep the degraded post-hoc-filter recall behavior above, with no signal that iterative scan was never actually applied. A version *string* check is the only way to know, ahead of the `SET`, whether it will actually do anything. `ALTER ROLE ... SET hnsw.iterative_scan = ...` has the identical problem for the same reason -- it's still a `SET` under a different scope.

Whether iterative scan is applied at all is gated by a new `vector_store.iterative_scan` config field (`*bool` on `config.VectorStore`, wired through `HNSWOptions.IterativeScan` in `internal/index`), following the same nil-default-true pattern as `vector_store.reindex_enabled` and `vector_store.reindex_concurrently`: unset or explicit `true` means "on if the installed pgvector supports it," explicit `false` disables it regardless of version. The wanted-vs-supported computation happens once in `NewPgStore`, before the pool is constructed, and the resulting boolean is captured by the `AfterConnect` closure -- every connection in the pool gets the same treatment, not just the first one. If the config wants it on but the installed pgvector doesn't support it, `NewPgStore` prints a one-time warning naming the detected pgvector version and pointing at this ADR (a distinct warning fires instead if the version probe itself fails); no warning fires when the user explicitly set `iterative_scan: false`.

pgvector's `hnsw.iterative_scan` GUC has two non-`off` modes: `strict_order`, which keeps exact distance ordering but scans conservatively (closer to no iterative scan at all in the presence of a post-hoc filter), and `relaxed_order`, which continues the graph walk more aggressively at the cost of not guaranteeing returned rows are in exact strict distance order. This decision uses `relaxed_order` rather than `strict_order`: `strict_order`'s ordering guarantee isn't worth its weaker recall recovery for this codebase's use case, where the filtered-out-candidate problem (not exact ranking) is the actual failure mode being fixed.

## Consequences

- Recall improves substantially at higher tenant/ADR counts: the same benchmark reports iterative-scan recall of 100% (1 project), 100%/86% (10 projects at 25/100 ADRs), and 98%/80% (50 projects at 25/100 ADRs) against the baseline table above.
- The cost is bounded latency, not unbounded: roughly break-even at low scale, up to ~2.3x p50 at the worst scale point measured (50 projects, 100 ADRs), still sub-4ms p50 in absolute terms. `PgStore.Search`'s query shape and `topK`/`similarity_threshold` parameters are unchanged, but `relaxed_order` trades a small amount of exact-distance-ordering precision for that recall improvement -- the returned top-K set is not guaranteed to be the mathematically exact top-K by cosine distance. This is an accepted tradeoff given the benchmark data above: the filtered-out-candidate recall loss it fixes is a larger, more visible problem than near-ties in ranking order among already-relevant ADRs.
- A pre-0.8.0 pgvector installation gets a printed warning instead of a silent capability downgrade, but the run still proceeds with the old (degraded) recall behavior -- this is a warn, not a fail, consistent with this codebase's existing warn-on-degraded-input conventions (e.g. the CI Warn-Open truncation policy in `internal/analysis`).
- Users who have measured iterative scan's latency cost as unacceptable for their scale/SLA can opt out via `vector_store.iterative_scan: false`, trading recall back down to the baseline table for lower and more predictable per-query latency.
- This only mitigates the recall loss described above; it doesn't restructure `archguard_adrs` into a per-project index or partition. A true multi-tenant index-partitioning scheme, if ever needed at higher scale than this GUC recovers, is a larger and separate change.
