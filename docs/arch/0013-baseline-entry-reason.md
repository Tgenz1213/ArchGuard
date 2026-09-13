---
title: "Baseline entry suppression reason"
status: "Accepted"
---

# Baseline entry suppression reason

## Context

`docs/arch/0006-violation-baseline-file.md` established `--update-baseline` as a snapshot mechanism: every currently-detected violation is written into `archguard-baseline.json`, keyed on `(ADR ID, file path)`, with no record of *why* it was suppressed. In practice a baselined entry is one of two very different things -- a violation a human looked at and decided is acceptable technical debt for now, or a case where the LLM was simply wrong (misread the ADR, hallucinated intent) and there's no real debt at all. Both looked identical in the baseline file, so there was no way to audit "what debt have we consciously accepted" separately from "what has ArchGuard just been wrong about," and a false positive baselined this way would keep quietly suppressing forever with nobody ever prompted to revisit it. See issue #157.

## Decision

`baseline.Entry` gains a `Reason string` field (`json:"reason,omitempty"`), populated informationally only -- `IsSuppressed` does not read it, so suppression behavior is unchanged from 0006. A baseline file written before this change (no `reason` key) still loads with `Reason` at its zero value, and still suppresses exactly as before.

`--update-baseline` accepts a new `--baseline-reason <text>` flag, applied to every entry the run collects. When the flag is omitted, an entry re-collected for an `(ADR ID, file)` pair that already existed in the previous baseline file keeps that entry's prior `Reason` rather than losing it -- otherwise 0006's "always a full snapshot, never a merge" semantics would silently erase a reason a human had manually curated on every routine re-run. A brand-new entry (no matching prior entry, no `--baseline-reason`) gets an empty `Reason`; nothing invents one.

Loading the previous baseline file for this reason lookup is best-effort: if the file is missing or corrupt, `--update-baseline` prints a warning and proceeds with no carried-forward reasons, rather than failing. `--update-baseline` remains the documented recovery path for a corrupt baseline file (0006 established this implicitly by never loading the old file for suppression purposes during an update run); making the *reason* lookup non-fatal preserves that property instead of accidentally coupling a new opt-in feature to an existing recovery guarantee.

`Reason` is surfaced in `check`'s human-readable output (a `Baseline Reason: <text>` line under `[BASELINED]`, and under `[VIOLATION]` during `--update-baseline`) whenever it's non-empty, so a reviewer scanning `check` output can see why a suppressed violation is suppressed without opening the baseline file.

## Consequences

- A team can now distinguish accepted debt from tool false-positives in `archguard-baseline.json`, either by hand-editing the `reason` field after a plain `--update-baseline`, or by re-running with `--baseline-reason` to apply one value across a whole update.
- No suppression behavior changed: an entry with any `Reason` value (including none) suppresses identically. This is a deliberate v1 scope decision from issue #157 -- a future ADR could give the two categories different behavior (e.g. `--debug` visibility, expiring false-positive suppressions) without touching this decision's `Entry` shape.
- Baseline suppression is still coarse-grained per 0006 (per `(ADR ID, file)`, not per violation instance); `Reason` doesn't change that, and a re-baselined entry with a different `QuotedCode` for the same key still carries forward whatever `Reason` the previous entry at that key had, even if the specific violation is now a different one than the human originally annotated. This is a known limitation, not a bug -- the same periodic-`--update-baseline` mitigation from 0006 applies here too.
