---
title: Per-ADR similarity_threshold override in frontmatter
status: Accepted
scope: "internal/**"
---

# Per-ADR similarity_threshold override in frontmatter

## Context

`vector_store.similarity_threshold` is one global value applied to every ADR's search hits. A broad architectural ADR and a narrow naming-convention ADR warrant different bars for whether a file is relevant to them, but the corpus could not express that. See #135.

## Decision

An ADR may set an optional `similarity_threshold` in its frontmatter. When the key is present, its value applies to that ADR, even an explicit zero. When it is absent, the global threshold applies.

The override is resolved in the one threshold predicate that both the accepted-candidate filter and the rejected-candidate filter share, as `docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md` anticipated. Both vector stores therefore apply it to both searches. Threshold filtering runs after scope filtering (`docs/arch/0007-scope-filtered-before-topk-similarity.md`), so each candidate's own override is known when its threshold is checked.

`--debug` output for a candidate below threshold prints the threshold that was applied, not the global value.

`PgStore` stores the override in its own column, added when missing in the same way as the `adr_id` and `scope` columns (#82). A change to only an ADR's threshold is synced as metadata without re-embedding the ADR, like a change to only its scope.

## Rules

- When an ADR's `similarity_threshold` key is present, its value MUST apply to that ADR, even an explicit zero.
- The override MUST be resolved in the one threshold predicate that both the accepted-candidate filter and the rejected-candidate filter share.
- `--debug` output for a candidate below threshold MUST print the threshold that was applied, not the global value.
- `PgStore` MUST sync a change to only an ADR's threshold as metadata without re-embedding the ADR.

## Alternatives considered

None recorded.

## Consequences

- An ADR author can tune how readily their own ADR becomes a candidate, independent of every other ADR, without touching `archguard.yaml`.
- Existing ADRs and configs are unaffected: an ADR with no `similarity_threshold` uses the global value.
- Same footgun as `scope`: the threshold lives in frontmatter, and `LocalStore`'s index hash excludes it. A threshold-only edit therefore does not trigger `check`'s automatic rebuild; a manual `archguard index` is needed for `LocalStore`, as for a scope-only edit.
- `PgStore` users upgrading from an install without the column get it on their next `archguard index`, with no manual migration.
