---
title: "VectorStore.Search Surfaces Query/Scan Failures Instead of Silently Truncating Results"
status: "Accepted"
---

# VectorStore.Search Surfaces Query/Scan Failures Instead of Silently Truncating Results

## Context

`PgStore.Search` had no way to signal a failure to its caller: the `VectorStore` interface's `Search` method returned only `[]SearchResult`, no error. A query failure was `fmt.Printf`'d and the method returned `nil`, indistinguishable from "no matches." A per-row `Scan` failure was `fmt.Printf`'d and the loop `continue`d — but pgx v5's `Scan()` failure ends row iteration entirely (`rows.fatal()` closes the rows), so every ADR ranked after the failed row, not just the bad one, silently vanished from the result set too. `Engine.Run`'s only caller saw a shorter or empty hit list with no signal anything went wrong, so a real architectural violation could go completely undetected with no error and no non-zero exit code, even in `--ci` mode.

`LocalStore.Search` (in-memory slice scan) has no analogous failure mode.

## Decision

`VectorStore.Search`'s signature changes to `Search(queryEmbedding []float32, threshold float64, topK int) ([]SearchResult, error)`. `PgStore.Search` returns a wrapped error immediately on a query failure or a per-row `Scan` failure, rather than returning partial results. `LocalStore.Search` always returns a nil error.

`Engine.Run` treats a `Search` failure exactly like a `fetchContext`/`CreateEmbedding` failure: log it, increment `SkippedFiles`, skip the file, continue to the next one. This is **not** gated behind `--ci` Warn-Open (unlike a truncated file, which is a data-completeness caveat about content that was successfully read). A `Search` failure means the engine couldn't determine relevant ADRs for the file at all — an infrastructure failure, the same class as `fetchContext`/`CreateEmbedding` failures, which already skip-and-count unconditionally in every mode.

## Consequences

A `Search` failure is now visible in output and counted in `--update-baseline`'s summary, instead of silently degrading recall with no signal. Every implementer of `VectorStore.Search` (currently `LocalStore` and `PgStore`) and every caller must handle the added error — a breaking interface change, but confined to a single small interface with two implementations and one production call site.

Fail-fast trades a wider blast radius for that visibility: one malformed row that ranks in a file's top-3 now skips that file's entire ADR check, not just the ranked-after hits the old code silently lost. Deliberate — proceeding on a partial, unexplained hit set risks missing violations just as silently as before, only now with corrupted-looking partial data instead of none.

**Known remaining gap** (not fixed here): `SkippedFiles` does not affect `archguard`'s exit code in any mode, including `--ci` — only `violations > 0` does. A `Search` failure (or a total backend outage) is now visible and counted, but a run where every file's `Search` call fails still exits `0`. This pre-exists this change for `fetchContext`/`CreateEmbedding` failures too; fixing it is a broader policy decision (should any skipped-file count be able to fail a `--ci` run?) tracked separately.

Rejected alternative: keep the signature unchanged and just improve the `fmt.Printf` wording. Rejected because that reproduces the exact bug being fixed — a `Search` failure would still be indistinguishable from "no relevant ADRs" to any caller, with no way to count it, warn on it, or fail a `--ci` run because of it.
