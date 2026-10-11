---
title: Single-query consistency for debug diagnostics
status: Accepted
scope: "internal/**"
---

# Single-query consistency for debug diagnostics

## Context

Under `--debug`, each file's hits, below-threshold candidates (`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md`) and candidates cut by the topK limit came from three separate queries. On Postgres with relaxed-order iterative HNSW scans (`docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md`), search is approximate, and two queries with the same vector need not return the same candidates. A `--debug` run could report an ADR as truncated when it was in fact a hit, or the reverse. Flagged by automated PR review on #192.

## Decision

`VectorStore` has `SearchWithDebugInfo`, which issues one query, applies the scope filter once, and derives the hits, the rejected candidates and the truncated candidates from that one candidate set. The cosine stage uses it whenever `--debug` is set and calls plain `Search` otherwise, so a normal run is unaffected, as 0009 requires.

`SearchRejected` and `SearchTruncated` stay on the interface as standalone methods. Their single-purpose shape is easier to reason about and test in isolation. Only the debug path, which needs its hits to match what it prints, uses the combined method.

## Rules

- `SearchWithDebugInfo` MUST issue one query, apply the scope filter once, and derive the hits, the rejected candidates and the truncated candidates from that one candidate set.
- The cosine stage MUST use `SearchWithDebugInfo` whenever `--debug` is set and plain `Search` otherwise.

## Alternatives considered

None recorded.

## Consequences

- `--debug` output is consistent by construction: an ADR reported as a hit, rejected or truncated always reflects the same query. The divergence is removed rather than accepted as a known limitation.
- On Postgres, `--debug` issues one query per file instead of three, with the same candidate cap.
- The local store's search is exact and has no consistency problem, but it shares the combined shape so the three-call pattern does not creep back in.
