---
title: Single-query consistency for debug diagnostics
status: Accepted
scope: "internal/**"
---

# Single-query consistency for debug diagnostics

## Context

`Engine.Run`'s `--debug` path called `Search`, `SearchRejected` (`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md`), and `SearchTruncated` (that ADR's amendment for #190) independently -- three separate queries per file. For `PgStore` under `hnsw.iterative_scan = 'relaxed_order'` (`docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md`), HNSW search is approximate: two separate queries against the same index with the same query vector are not guaranteed to return identical candidate sets. Within a single `--debug` run, this could misreport an ADR as truncated despite it actually being a hit (or the reverse), since the diagnostic lines were computed from a different query than the one that produced the actual analyzed `hits`. Flagged by automated PR review on #192.

## Decision

`VectorStore` gains `SearchWithDebugInfo(queryEmbedding []float32, threshold float64, topK int, filePath string) (hits, rejected, truncated []SearchResult)`, which issues exactly one query, applies `filterByScope` once, then derives all three outputs from that single candidate set -- a distinct copy per output, since `filterByThreshold`/`filterBelowThreshold` filter their input in place: `rejected` from `filterBelowThreshold` + `rankAndLimit`, `hits` and `truncated` from `filterByThreshold` followed by `rankAndLimit` and `truncatedByTopK` respectively.

`CosineRanker` (`internal/analysis/stage/cosine.go`) calls `SearchWithDebugInfo` instead of `Search`+`SearchRejected`+`SearchTruncated` whenever `--debug` is set. Outside `--debug` it calls plain `Search` alone, so non-debug behavior and cost are unaffected, same as `docs/arch/0009`'s "non-debug performance unchanged" requirement.

`SearchRejected` and `SearchTruncated` are kept as standalone methods -- still part of the `VectorStore` interface, still directly tested -- since other callers/tests use them independently and their single-purpose shape is easier to reason about in isolation. Only `CosineRanker`'s debug path, which needs `hits` to stay consistent with what it prints, uses the consolidated method.

## Consequences

- `--debug` output is now internally consistent by construction: an ADR reported as a hit, rejected, or truncated always reflects the same query's candidate set, eliminating the cross-query divergence risk entirely rather than accepting it as a known limitation.
- `PgStore.SearchWithDebugInfo` issues one query per file in `--debug` mode (instead of three `Search`/`SearchRejected`/`SearchTruncated` queries), bounded by the same `MaxSearchCandidates` cap as those methods -- a net reduction in debug-mode query volume, not just a correctness fix.
- `LocalStore.SearchWithDebugInfo` has no correctness implications (exact, non-approximate computation), but shares the same consolidated shape as `PgStore`'s for interface symmetry and to avoid the three-independent-call pattern creeping back in via a future edit.
