---
title: Index corpus health reporting
status: Accepted
scope: "internal/index/**,internal/cli/**"
---

# Index corpus health reporting

## Context

`archguard index`'s only terminal output was a valid-ADR count, a progress dot per embed, and a generic "ADR Index updated successfully" regardless of how many ADRs were silently excluded along the way. `LocalProvider.GetADRs` printed a warning per parse failure with no aggregation, and status-rejected ADRs produced zero output at all. There was also no detection of duplicate ADR IDs across the corpus, which silently breaks `archguard-ignore`/baseline suppression scoping (both key on ADR ID). See #136.

## Decision

`index.Provider.GetADRs` now returns `([]ADR, FetchStats, error)` instead of `([]ADR, error)`. `FetchStats` (`internal/index/provider.go`) carries what a provider found beyond the valid ADRs themselves: `Discovered` (total files/pages seen), `ParseFailed` (paths), and `StatusRejected` (count). `LocalProvider` and `ConfluenceProvider` populate it as they walk/paginate; `CompositeProvider` sums it across providers the same way it already merges `[]ADR`. The duplicated "filter by status" loop in both providers was extracted into a shared `isAcceptedStatus` helper in `provider.go`.

Duplicate-ID and no-scope detection have no per-provider signal — a collision can span providers (a local file and a Confluence page sharing an ID) — so they're computed once, post-merge, in a new `internal/index/health.go`. `summarizeCorpus(validADRs, stats) IndexSummary` builds `IndexSummary{Discovered, Valid, ParseFailed, StatusRejected, DuplicateIDs, NoScope}` from the final valid-ADR slice plus the merged `FetchStats`. `BuildIndexResult` (`internal/index/store.go`) now embeds `IndexSummary` alongside its existing `Skipped []SkippedADR` (embed failures); both `LocalStore.BuildIndex` and `PgStore.BuildIndex` populate it identically via `summarizeCorpus`.

`internal/cli.runIndex` prints a structured summary (`printIndexSummary`) instead of the old pass/fail line, and calls the new `IndexSummary.IsEmpty()` (`Valid == 0`) to decide the exit code: `ExitIndexError` when nothing survived to be indexed, `ExitSuccess` otherwise (skips/rejections/duplicates are reported but don't fail the run). `IsEmpty` checks `Valid == 0` regardless of `Discovered`, not just "discovered something but all of it got rejected" — a `Discovered == 0` corpus (e.g. a misconfigured `adr_path`) is exactly the case where `PgStore.BuildIndex`'s existing delete-missing-ADRs step would otherwise wipe out a previously-healthy index while still reporting success.

## Consequences

- A user now sees, after every `archguard index`, exactly what happened to every discovered ADR: valid, parse-failed, status-rejected, failed-to-embed, or a duplicate-ID collision — closing the "no way to tell if the corpus is healthy" gap from #136.
- `archguard index` (and, transitively, `archguard check --ci`'s auto-rebuild-on-hash-mismatch path) now exits non-zero for a corpus with zero valid ADRs, including a directory with nothing in it yet. This is deliberate: a fresh `archguard init` scaffolds `ADR_TEMPLATE.md` with placeholder (non-matching) `status`, so running `index` before writing a real ADR now surfaces that loudly instead of a silent, always-passing `check`.
- `Provider` is a small interface with few implementers (`LocalProvider`, `ConfluenceProvider`, `CompositeProvider`, plus test mocks), so the signature change's blast radius was contained to those and their direct callers in `internal/cli` and `internal/index/store.go`/`pgvector.go`.
