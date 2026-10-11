---
title: HNSW iterative scan for project-filtered search
status: Accepted
scope: "internal/**"
---

# HNSW iterative scan for project-filtered search

## Context

Every project's ADRs live in one Postgres table with one shared HNSW index, and search filters by project after the graph walk. HNSW stops walking once it has `topK` candidates, however many of them survive the project filter. A benchmark built for #44 shows recall falling as tenants share the index:

| Scale | Baseline recall |
|---|---|
| 1 project | 100% |
| 10 projects, 25 ADRs | 88% |
| 10 projects, 100 ADRs | 72.7% |
| 50 projects, 25 ADRs | 28% |
| 50 projects, 100 ADRs | 23.3% |

At 50 projects a query returns fewer than one relevant ADR on average, out of 3 requested. The loss is silent: the engine cannot tell a search came back incomplete, so real drift goes unflagged with no error or warning.

## Decision

The Postgres store sets pgvector's `hnsw.iterative_scan = 'relaxed_order'` on every pooled connection. The graph walk then continues until it has `topK` candidates that also pass the project filter, up to pgvector's own scan cap.

The setting exists only from pgvector 0.8.0, so the store checks the installed extension version before setting it. The check is required: Postgres accepts `SET` of an unknown setting without error, so setting it and catching the error would silently keep the degraded recall on an older install. `ALTER ROLE ... SET` has the same problem. A version check is the only way to know in advance whether the setting takes effect.

The `vector_store.iterative_scan` config key gates it. Unset or `true` means on when pgvector supports it; `false` turns it off on any version. When the config wants it but pgvector is too old, the store prints a one-time warning naming the detected version and pointing at this ADR. No warning fires when the user turned it off.

`relaxed_order` is chosen over `strict_order`. `strict_order` keeps exact distance order but scans conservatively, close to no iterative scan under a post-hoc filter. The failure being fixed is filtered-out candidates, not exact ranking, so the ordering guarantee is not worth the weaker recall.

## Consequences

- Recall recovers on the same benchmark: 100% at 1 project, 100% and 86% at 10 projects (25 and 100 ADRs), 98% and 80% at 50 projects.
- Latency cost is bounded: about break-even at low scale, up to about 2.3x p50 at 50 projects with 100 ADRs, still under 4ms p50.
- `relaxed_order` does not guarantee the exact top-K by cosine distance. Near-ties in ranking among relevant ADRs are a smaller problem than the recall loss this fixes.
- A pgvector older than 0.8.0 gets a warning, and the run proceeds with degraded recall. This is warn, not fail, consistent with the codebase's other warn-on-degraded-input policies such as CI Warn-Open truncation.
- A user who finds the latency cost unacceptable can set `vector_store.iterative_scan: false`, trading recall back for lower, more predictable latency.
- This mitigates the recall loss without restructuring the table into per-project indexes or partitions. True multi-tenant partitioning, if a larger scale ever needs it, is a separate change.
