---
title: An oversized file is judged by its diff before it is truncated
status: Accepted
scope:
  - "internal/analysis/**"
---

# An oversized file is judged by its diff before it is truncated

## Context

A file larger than `llm.max_tokens` cannot be sent whole. Truncating it keeps the top of the file and drops the rest, so the lines a developer just changed are often the ones the model never sees. The changed lines are what a check of uncommitted or staged work is about.

## Decision

When a file is too large to send whole, `check` sends the model the file's diff instead: its uncommitted or staged change, or its change since the ref under `--since --whole-file`. The file is truncated only when it has no diff. A quote from the model is located in the new file's lines of that diff. Under `--update-baseline` the diff is never used, because a baseline entry quotes code from the whole file (`docs/arch/0006-violation-baseline-file.md`). A plain `--since` check reads the diff anyway (`docs/arch/0030-judge-the-change-under-since.md`).

## Rules

- Code that truncates a file too large to send whole when the file has a diff and the run is not `--update-baseline`.
- Code that sends the diff instead of the whole file under `--update-baseline`.

## Alternatives considered

- **Always truncate.** It is simpler, but it judges the part of the file least likely to have changed.

## Consequences

- A large file with a diff is judged on the change, not cut short, so it is not reported as partly checked.
- A large file with no diff, such as an unchanged one checked with `--all`, is still truncated, and `--ci` still skips it (`docs/arch/0028-check-report-and-log.md`).
