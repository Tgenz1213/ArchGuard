---
title: Configurable ADR ID extraction pattern
status: Accepted
scope: "internal/**"
---

# Configurable ADR ID extraction pattern

## Context

`ParseADR` derives an ADR's ID by splitting its filename on the first hyphen, assuming the `NNNN-title.md` convention. A corpus using a different naming convention, e.g. `adr-1-use-postgres.md` and `adr-2-use-kafka.md`, has every file collapse to the same ID (`adr`), silently colliding IDs across unrelated ADRs. See #154.

## Decision

`config.Analysis` gains an optional `ADRIDPattern string` field (`adr_id_pattern` in YAML). `internal/cli.compileADRIDPattern` compiles it once at startup, right after `validateProviderConfig`, so a bad regex fails fast with `ExitConfig` instead of surfacing per-file during a scan. An unset field compiles to `(nil, nil)`, preserving today's behavior byte-for-byte.

`ParseADR` takes the compiled pattern as a new third parameter and delegates ID extraction to `extractID(filename string, idPattern *regexp.Regexp) string`: when the pattern matches, capture group 1 is used if the pattern defines one, otherwise the whole match; when `idPattern` is nil or doesn't match a given filename, extraction falls back to the existing first-hyphen split. The fallback is per-file, not corpus-wide, so a mixed-convention corpus (some files matching the custom pattern, others not) still gets a sensible ID for every file rather than failing outright.

`LocalProvider` stores the compiled pattern via a new `SetIDPattern(re *regexp.Regexp)` setter (nil is also the zero value `NewLocalProvider` starts with) and passes it through to `ParseADR` in `GetADRs`. `internal/cli.Execute` compiles the pattern once and threads it through both `runCheck` and `runIndex`, each of which calls `SetIDPattern` on the `LocalProvider` it constructs.

`ConfluenceProvider` is unaffected: it derives ADR IDs from Confluence page IDs, not filenames, so this option has no meaning there.

## Consequences

- Corpora using a naming convention other than `NNNN-title.md` can now get distinct, stable ADR IDs by setting `analysis.adr_id_pattern`, without renaming files.
- Existing configs are unaffected: an unset `adr_id_pattern` compiles to a nil pattern, and `extractID` with a nil pattern is exactly the old `strings.Split(filename, "-")[0]` logic.
- A pattern that doesn't match a particular file doesn't error — that file just falls back to the default split — so introducing `adr_id_pattern` for a subset of a corpus's files doesn't require every file to conform to it.
- A malformed regex is caught once at startup (`ExitConfig`), not per-ADR during indexing or checking, keeping the failure mode consistent with other config validation.
- `LocalStore.CalculateHash` includes each ADR's `ID` alongside `RelPath`/`Content`, so adding or changing `adr_id_pattern` on an existing repo changes the computed hash and triggers `runCheck`'s automatic rebuild-on-mismatch -- no manual `archguard index` is needed for the new IDs to take effect.
