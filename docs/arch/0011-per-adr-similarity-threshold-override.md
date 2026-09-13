---
title: Per-ADR similarity_threshold override in frontmatter
status: Accepted
scope: "internal/**"
---

# Per-ADR similarity_threshold override in frontmatter

## Context

`vector_store.similarity_threshold` is a single global value applied uniformly to every ADR's search-hit filtering. A broad architectural ADR and a narrow naming-convention ADR warrant different bars for "is this file even relevant to this ADR," but the corpus had no way to express that. See #135.

## Decision

`ADR` gains an optional `SimilarityThreshold *float64` field, parsed from a new `similarity_threshold` frontmatter key alongside the existing `scope` field. A new function, `index.EffectiveThreshold(adr *ADR, global float64) float64`, resolves it: the ADR's own value when set, otherwise the caller's global threshold.

`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md` already anticipated this: `filterByThreshold` and `filterBelowThreshold` both key off one `meetsThreshold` predicate specifically so a future threshold-semantics change would need to happen in one place. `meetsThreshold` now calls `EffectiveThreshold` instead of comparing directly against the global value, so both filters — and therefore both `LocalStore` and `PgStore`'s `Search`/`SearchRejected` — pick up per-ADR overrides with no other call-site change. Per `docs/arch/0007-scope-filtered-before-topk-similarity.md`, threshold filtering already runs after scope filtering in both backends, so a per-ADR threshold is evaluated only once the candidate's identity (and therefore its own override) is known — this ordering was already required for the global-threshold case and needed no rework.

`Engine`'s `--debug` "Below threshold" log line is updated to print `EffectiveThreshold(r.ADR, threshold)` instead of the raw global `threshold`, so debug output reflects the value that was actually applied.

`PgStore` gains a `similarity_threshold DOUBLE PRECISION` column, added via the same ALTER-if-missing mechanism `ensureSchema` already uses for `adr_id`/`scope` (see #82's fix, referenced throughout `pgvector.go`). It's selected in `BuildIndex`'s existing-row fetch and in `SearchQuery`, written on insert/upsert, and folded into the existing `adrsToSync` metadata-only sync path (`existing.ID != valid.ID || existing.Scope != valid.Scope || !thresholdsEqual(existing.SimilarityThreshold, valid.SimilarityThreshold)`) via a new `thresholdsEqual(*float64, *float64) bool` helper, so a similarity_threshold-only frontmatter edit — like a scope-only edit — is picked up without a re-embed.

`LocalStore` needed no production code change: its `BuildIndex` already assigns `finalADRs` directly from the freshly-parsed ADR slice regardless of which fields changed, and JSON marshaling already round-trips every `ADR` field via struct tags.

## Consequences

- An ADR author can now tune how aggressively their own ADR is surfaced as a candidate, independent of every other ADR in the corpus, without touching `archguard.yaml`.
- Existing ADRs and configs are unaffected: an ADR with no `similarity_threshold` frontmatter key parses to a nil `SimilarityThreshold`, and `EffectiveThreshold` falls back to the global value exactly as before this change.
- Same footgun class as `scope`: the per-ADR value lives in frontmatter, not `Content`, so a threshold-only edit doesn't change `LocalStore.CalculateHash` (which hashes `RelPath`+`Content` only) and won't trigger `runCheck`'s automatic hash-mismatch rebuild — a manual `archguard index` is needed to pick it up for `LocalStore`, same as an existing `scope`-only edit already requires.
- `PgStore` users upgrading from a pre-#135 install pick up the new column automatically on their next `archguard index` (same as the #82 `adr_id`/`scope` migration) — no manual schema migration needed.
