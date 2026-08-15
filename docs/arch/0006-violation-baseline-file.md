---
title: "Violation baseline file"
status: "Accepted"
---

# Violation baseline file

## Context

`archguard check` fails a pipeline (or a local pre-commit hook) as soon as it finds any violation of an applicable ADR. On a codebase adopting ArchGuard for the first time, that's often not one or two violations but dozens, scattered across files nobody plans to touch today. Requiring every one of them to be fixed before `check` can pass at all is an adoption blocker: a team either can't turn ArchGuard on, or has to carve out a large one-off cleanup effort before it can. What's needed is a way to grandfather in the violations that already exist, while still catching *new* ones on every subsequent run.

## Decision

`archguard check --update-baseline` scans the whole repository and writes every currently-detected violation into `archguard-baseline.json` at the repo root, replacing the file wholesale (a snapshot, not a merge with whatever it contained before — running it again always reflects the current state of the repo, not an accumulation of past states). A normal `archguard check` run auto-loads this file (no extra flag) and treats a matching violation as already-known: excluded from the new-violation count and exit code, but still printed as `[BASELINED]` rather than silently dropped.

Suppression is keyed on `(ADR ID, file path)`, not line number. `findLineNumber` (used only for the human-readable `[Line %d]` annotation) locates a quote via a plain substring search, which shifts under any edit above the matched line and isn't a stable identity for a violation across runs — using it as the suppression key would silently unsuppress or missuppress entries on unrelated edits elsewhere in the file.

An entry stores the LLM's `QuotedCode` for that violation and is invalidated -- its violation re-surfaces as new drift -- once `QuotedCode` is no longer a substring of the file's current content. This was chosen over comparing the LLM's output run-to-run (e.g. re-running analysis and diffing results): LLM output isn't deterministic even at temperature 0 across model versions or provider updates, so a re-baseline could drift for reasons having nothing to do with the code actually changing. Checking "is the cited code still there" is a purely mechanical, deterministic condition tied to the one thing that actually matters: has the violating code changed.

An entry with an empty `QuotedCode` (the LLM flagged a violation without quoting a specific snippet) is treated as always suppressed rather than never suppressed. Failing open (never suppressed) would mean every such entry immediately reappears as new drift on the very next run, defeating the point of baselining it in the first place; failing closed accepts that a small subset of entries can't auto-invalidate via content-matching; a user can still force them to re-surface by re-running `--update-baseline` after fixing the underlying issue.

`archguard-baseline.json` is committed to git at the repo root, unlike `.archguard/index.json` and `.archguard/cache/`, which are gitignored local artifacts. A baseline is a statement about the *codebase's* grandfathered state, not a given developer's local environment: every CI runner and every teammate's clone needs to see the same suppression set, which a gitignored, machine-local file can't provide. The self-referential risk this creates -- the file's own content is quoted violating source, so it could itself get flagged and baselined into a spurious growing loop -- is handled by unconditionally excluding it from analysis in `Engine.shouldExclude`, independent of the user's `exclude_patterns` config.

## Consequences

- A legacy codebase can adopt ArchGuard immediately via `--update-baseline`, then rely on ordinary `check` runs to gate only *new* violations going forward.
- Suppression is coarser than per-violation: a second, different violation of an already-baselined `(ADR, file)` pair is silently swallowed until the originally-cited code changes. This is a known, documented limitation (see CLAUDE.md), not a bug -- mitigated by periodically re-running `--update-baseline`.
- Because the baseline is content-keyed rather than line-keyed, unrelated edits elsewhere in a file never spuriously unsuppress an entry, but an edit to the violating line itself -- even a trivial reformat -- does, which is the intended behavior.
- Issue #75 (Postgres-backed baseline storage) is an explicitly planned extension of this decision, not a replacement: it moves *where* `Baseline` is persisted (a shared Postgres table instead of a git-committed JSON file) for teams that want centralized, always-current baseline state rather than one that travels with a git ref, but the `(ADR ID, file path)` suppression key and QuotedCode-substring invalidation rule established here are expected to carry over unchanged.
