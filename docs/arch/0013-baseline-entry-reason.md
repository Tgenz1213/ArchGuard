---
title: "Baseline entry suppression reason"
status: "Accepted"
scope: "internal/baseline/**"
---

# Baseline entry suppression reason

## Context

`docs/arch/0006-violation-baseline-file.md` makes `--update-baseline` a snapshot: every detected violation is written to `archguard-baseline.json`, keyed on ADR ID and file path, with no record of why it was suppressed. A baselined entry is one of two different things. Either a human decided the violation is acceptable debt for now, or the LLM was wrong and there is no debt at all. Both look the same in the file, so a team cannot audit the debt it accepted separately from the tool's mistakes, and a baselined false positive suppresses forever with nobody prompted to revisit it. See #157.

## Decision

A baseline entry has an optional `reason`. It is informational only: suppression does not read it, so suppression is unchanged from 0006. A baseline file without `reason` keys loads and suppresses as before.

`--update-baseline` accepts `--baseline-reason <text>`, applied to every entry the run collects. Without the flag, an entry re-collected for an ADR and file already in the previous baseline keeps that entry's reason. Otherwise 0006's full-snapshot semantics would erase a hand-curated reason on every routine re-run. A new entry with no flag gets an empty reason; nothing invents one.

Reading the previous baseline for its reasons is best-effort. If the file is corrupt or unreadable, `--update-baseline` prints a warning and carries no reasons forward rather than failing. `--update-baseline` is the recovery path for a corrupt baseline file (0006 never loads the old file for suppression during an update), and an opt-in feature must not break that guarantee.

`check`'s human-readable output shows a non-empty reason under a baselined violation, and under a violation during `--update-baseline`, so a reviewer sees why it is suppressed without opening the baseline file.

## Rules

- Suppression MUST NOT read a baseline entry's `reason`.
- A baseline file without `reason` keys MUST load and suppress as before.
- Without `--baseline-reason`, an entry re-collected for an ADR and file already in the previous baseline MUST keep that entry's reason.
- A new entry with no `--baseline-reason` MUST get an empty reason.
- If the previous baseline is corrupt or unreadable, `--update-baseline` MUST print a warning and carry no reasons forward rather than fail.

## Alternatives considered

None recorded.

## Consequences

- A team can tell accepted debt from tool false positives in `archguard-baseline.json`, by hand-editing `reason` after a plain `--update-baseline` or by re-running with `--baseline-reason` to set one value across an update.
- Suppression does not depend on the reason. This is a deliberate first-version scope from #157. A future ADR could treat the two kinds differently, such as showing them under `--debug` or expiring false-positive suppressions, without changing the entry's shape.
- Suppression stays coarse-grained per 0006, by ADR ID and file, not per violation. A re-baselined entry whose quoted code differs still carries forward the reason of the previous entry for that key, even if the violation is not the one a human annotated. This is a known limitation, and 0006's mitigation of re-running `--update-baseline` periodically applies.
