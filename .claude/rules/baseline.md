---
paths:
  - "internal/baseline/**"
  - "archguard-baseline.json"
---

# `internal/baseline`

Stores and loads `archguard-baseline.json` (path is the `Path` constant, always the repo root, not configurable) via the `Baseline`/`Entry` types. `check --update-baseline` scans the whole repo and calls `Baseline.Save`, which fully replaces the file (a snapshot, not a merge with any prior baseline). A normal `check` run auto-loads it (no flag needed); `Baseline.IsSuppressed(adrID, file, currentContent)` keys on the `(ADR ID, file path)` pair and suppresses a matching violation as long as its stored `Entry.QuotedCode` is still a substring of `currentContent` — once that code is no longer present, the entry is invalidated and the violation re-surfaces as new drift. See `docs/arch/0006-violation-baseline-file.md`. An entry may also carry an informational `Reason` (`"accepted-debt"` vs `"false-positive"`, or free text) via `--update-baseline --baseline-reason <text>`; `IsSuppressed` ignores it entirely, and a plain `--update-baseline` re-run carries a matching entry's prior `Reason` forward rather than erasing it (`Baseline.ReasonFor`). See `docs/arch/0013-baseline-entry-reason.md`.

## Footguns

- **Baseline suppression is per `(file, ADR)` pair, not per violation.** `Baseline.IsSuppressed` has no notion of "which specific instance" — once one violation of a given ADR in a given file is baselined, a second, different violation of that same ADR in that same file is silently swallowed too, until the originally-baselined `QuotedCode` changes. This is a real limitation to know about before relying on baseline mode for legacy-codebase adoption; periodically re-running `--update-baseline` is the mitigation. See `docs/arch/0006-violation-baseline-file.md`.
- **A baseline entry's `Reason` follows the `(ADR ID, file)` key, not the specific violation.** If a re-baselined entry's `QuotedCode` changes to a different violation at the same key, it still carries forward whatever `Reason` was previously recorded there (unless `--baseline-reason` is passed) -- the same per-key coarseness `docs/arch/0006-violation-baseline-file.md` already documents for suppression applies to `Reason` too. See `docs/arch/0013-baseline-entry-reason.md`.
- **`--baseline-reason` is an all-or-nothing overwrite for the whole run, not a per-entry annotation.** It's applied to every entry `--update-baseline` collects, so re-running `archguard check --update-baseline --baseline-reason X` to annotate one newly-baselined violation silently overwrites every other entry's previously-curated `Reason` with `X` too.
