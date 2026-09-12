---
title: "BuildIndex isolates per-ADR embedding failures"
status: "Accepted"
scope: "internal/**"
---

# BuildIndex isolates per-ADR embedding failures

## Context

`LocalStore.BuildIndex` and `PgStore.BuildIndex` embedded new/modified ADRs concurrently via `errgroup.Wait()`, which returns the first non-nil error and cancels the group's derived context. One ADR whose content the embedding provider rejected (e.g. an oversized ADR exceeding the provider's input token limit) aborted the entire build: `internal/cli.runIndex` treated any `BuildIndex` error as fatal (`ExitIndexError`), and `runCheck`'s hash-mismatch auto-rebuild path meant a single bad ADR could block `archguard check` outright, not just `archguard index`. For `PgStore` this was worse: sibling goroutines' `INSERT ... ON CONFLICT` calls had often already committed before the group observed the error and stopped, leaving the database in an unspecified partially-synced state with no signal distinguishing that from a clean run.

## Decision

`BuildIndex` (both backends) treats a single ADR's `CreateEmbedding` failure (and, for `PgStore`, its subsequent `INSERT ... ON CONFLICT` failure) as non-fatal. The concurrent embedding goroutines never return a non-nil error to their `errgroup.Group` -- doing so would cancel the group and abort every sibling embed still in flight -- so an `errgroup.Group` here exists purely to bound concurrency (`SetLimit`), not to propagate failure. Each failure is instead recorded into a mutex-guarded `BuildIndexResult.Skipped []SkippedADR` (relative path + underlying error), and the failing ADR is excluded from this run's result rather than partially applied: `LocalStore` drops it from the in-memory corpus; `PgStore` simply never issues the `INSERT`, leaving any prior row for that ADR exactly as it was before this run.

`LocalStore.Hash` is computed from the full fetched ADR set, not from the successfully-embedded subset. The hash answers "does this saved index correspond to the ADR files currently on disk", which a partial index still does; *which* of those files embedded successfully is `BuildIndexResult.Skipped`'s job. Hashing the reduced set instead would make `runCheck`'s saved hash permanently disagree with its freshly-computed one whenever an ADR keeps failing, so every `check` would rebuild, still skip that ADR, fail to reload, and exit `ExitIndexError` -- an infinite rebuild loop that defeats this whole decision. The accepted trade-off: once a failing ADR's content stops changing, `check` stops auto-triggering rebuilds for it; a manual `archguard index` re-attempts every ADR regardless, since it builds a fresh store and never `Load`s first.

The `VectorStore.BuildIndex` interface signature changed from `(...) error` to `(...) (BuildIndexResult, error)`. The returned `error` is now reserved for build-wide failures that make the whole run meaningless -- fetching the ADR corpus, reaching the database, calculating the corpus hash -- never for an individual ADR's embedding failure. Two failure shapes are deliberately classified as build-wide rather than as a pile of per-ADR skips. A canceled `ctx` (Ctrl-C, or a caller's deadline) fails every in-flight and queued embed at once; it is checked immediately after the embed loop so it surfaces as one real error instead of N "skipped" ADRs plus a truncated index reported as success. And if *every* ADR that needed embedding failed, `BuildIndex` returns an error without touching the stored index at all -- an empty corpus would otherwise load cleanly and silently pass every file at exit 0, which is a strictly worse failure than the abort this ADR replaced. `internal/cli.runIndex` uses `BuildIndexResult.Skipped` to print which ADR(s) were skipped and why, and to avoid claiming unqualified success ("ADR Index updated successfully") when something was in fact skipped, while still returning `ExitSuccess` as long as the build itself completed.

## Consequences

- A single oversized or otherwise-rejected ADR no longer takes down indexing for every other ADR in the corpus, nor does it block `archguard check` via the hash-mismatch auto-rebuild path.
- `PgStore` can no longer end up with an unspecified partially-synced database from one bad ADR: a failed ADR's row (new or existing) is left exactly as it was; only successfully-embedded ADRs are written.
- Callers that only care about hard failures can keep checking the returned `error`; callers that want visibility into partial success must inspect `BuildIndexResult.Skipped` -- silently ignoring it (as `runIndex` explicitly does not) would reintroduce a milder version of the original problem: claiming success while data is quietly missing.
- A total embedding outage is still loud: it fails the build rather than quietly installing an empty index that would report every file clean. Partial success is tolerated; total failure is not.
- `PgStore.BuildIndex`'s HNSW reindex-churn accounting (`vector_store.reindex_threshold`) now counts only ADRs actually written this run, not every ADR *attempted* -- a run with many skipped ADRs and few successful writes correctly looks like low churn, not high churn.
