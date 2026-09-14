---
title: ADR scope frontmatter accepts a single glob or a list of globs
status: Accepted
scope: "internal/index/**"
---

# ADR scope frontmatter accepts a single glob or a list of globs

## Context

`ADR.Scope` (`internal/index/adr.go`) was a single glob string, matched via `MatchGlob` in `filterByScope` (`internal/index/rank.go`). An ADR that should apply to more than one unrelated path shape had no way to express that short of a single overly-broad glob or duplicating the ADR under multiple IDs, fragmenting its baseline/suppression history. See #171.

## Decision

`FrontMatter.Scope` and `ADR.Scope` change type from `string` to a new `index.ScopePatterns` (`[]string` underneath). `ScopePatterns.UnmarshalYAML` accepts either a scalar string (unchanged single-pattern behavior) or a YAML sequence of strings; `filterByScope` now calls `ScopePatterns.Matches(filePath)`, which is true if any pattern matches (OR semantics) or if the list is empty (unrestricted, same as today's `Scope == ""`).

Persistence for both backends stays schema-free:
- `LocalStore`'s JSON index file: `ScopePatterns.MarshalJSON`/`UnmarshalJSON` encode a single pattern as a bare JSON string (byte-identical to the pre-#171 format) and multiple patterns as a JSON array, so existing `.archguard/index.json` files keep loading with no migration.
- `PgStore`'s `scope TEXT` column: `ScopePatterns` implements `database/sql`'s `Valuer`/`Scanner` (`Value`/`Scan`), so it works as a drop-in query parameter and scan destination against the existing column with no ALTER. A single pattern stores as raw text exactly as before; multiple patterns store as a JSON array string in the same column. `ParseScopePatterns` is the shared inverse used by both `Scan` and any place a `TEXT` value is read into a local `string` before being packed back into an `ADR` (e.g. `BuildIndex`'s existing-row fetch).

`health.go`'s unscoped-ADR detection changes from `adr.Scope == ""` to `len(adr.Scope) == 0` — same meaning, updated for the new type.

## Consequences

- An ADR author can write `scope: ["a/**", "b/**"]` (or the equivalent YAML block-sequence form) to apply one ADR to multiple unrelated path shapes, without over-broadening a single glob or duplicating the ADR.
- No regression: every existing single-string `scope` value in an ADR file, an `.archguard/index.json`, or a `PgStore` row parses identically to before this change.
- `PgStore.BuildIndex`'s metadata-sync comparison (`existing.Scope != valid.Scope`) becomes a `slices.Equal` comparison, since `ScopePatterns` is a slice type and no longer directly comparable with `!=`.
