---
title: "Violation baseline file"
status: "Accepted"
scope:
  - "internal/baseline/**"
  - "internal/analysis/**"
  - "internal/cli/**"
---

# Violation baseline file

## Context

`archguard check` fails a pipeline or pre-commit hook on any violation of an applicable ADR. A codebase adopting ArchGuard often has dozens of existing violations in files nobody plans to touch. Requiring all of them fixed before `check` can pass blocks adoption. Teams need to grandfather in existing violations while still catching new ones on every run.

## Decision

`archguard check --update-baseline` scans the whole repository and writes every detected violation to `archguard-baseline.json` at the repo root. It replaces the file wholesale: a snapshot of the current state, not a merge with past states. A normal `check` loads the file without a flag and treats a matching violation as known. A known violation is excluded from the new-violation count and the exit code, but is still reported as baselined rather than dropped.

Suppression is keyed on `(ADR ID, file path)`, not line number.

An entry stores the violation's quoted code and is invalidated, re-surfacing as new drift, once the file stops containing that quote. "Is the cited code still there" is mechanical, deterministic and tied to the one thing that matters: whether the violating code changed.

An entry with an empty quote is always suppressed. Always suppressing accepts that these entries cannot auto-invalidate; re-running `--update-baseline` after a fix clears them.

`archguard-baseline.json` is committed to git, unlike the local index and cache, which are gitignored. A baseline states the codebase's grandfathered state, so every CI runner and every clone must see the same set. Because the file quotes violating source, it could itself be flagged and baselined in a growing loop. Analysis always excludes it, whatever the user's `exclude_patterns` say.

## Rules

- `archguard check --update-baseline` MUST replace `archguard-baseline.json` wholesale, not merge it with past states.
- A known violation MUST be excluded from the new-violation count and the exit code, but MUST still be reported as baselined rather than dropped.
- Suppression MUST be keyed on `(ADR ID, file path)`, not line number.
- A baseline entry MUST be invalidated, re-surfacing as new drift, once the file stops containing its quoted code.
- A baseline entry with an empty quote MUST always be suppressed.
- Analysis MUST always exclude `archguard-baseline.json`, whatever the user's `exclude_patterns` say.

## Alternatives considered

- **Key suppression on line number.** The line number comes from a plain substring search for the quote, so it shifts under any edit above the match. As a key it would silently unsuppress or wrongly suppress entries on unrelated edits.
- **Compare the LLM's output run to run.** LLM output is not deterministic across model versions or provider updates, even at temperature 0, so a baseline could drift with no code change.
- **Never suppress an entry with an empty quote.** Every such entry would reappear on the next run, defeating the baseline.

## Consequences

- A legacy codebase can adopt ArchGuard at once with `--update-baseline`, then gate only new violations.
- Suppression is coarser than per violation: a second, different violation of an already-baselined `(ADR, file)` pair stays hidden until the originally cited code changes. This is a known limitation, mitigated by re-running `--update-baseline` periodically.
- Unrelated edits elsewhere in a file never unsuppress an entry. An edit to the violating code itself, even a reformat, does, by intent.
- Issue #75 (Postgres-backed baseline storage) is a planned extension, not a replacement. It changes where the baseline is stored, for teams that want centralized state instead of state that travels with a git ref. The `(ADR ID, file path)` key and the quoted-code invalidation rule are expected to carry over unchanged.
