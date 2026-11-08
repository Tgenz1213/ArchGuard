---
title: A per-ADR ask node skips a file too large to screen
status: Accepted
scope:
  - "internal/analysis/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# A per-ADR ask node skips a file too large to screen

## Context

An `ask` node never reads more than `llm.max_tokens` (`docs/arch/0053-quoteless-runners-read-both-sides-of-a-diff.md`), but the judge can read more. A file the judge reads whole that is over that limit can't be screened. A node that asks per rule only narrows ADRs that have rules, so passing every candidate on costs one judgment per ADR at most. A node that asks per ADR can be the first node and then holds every scope-matched ADR: passing them all on makes a judgment per ADR, and keeping none drops the file's violations unreported.

## Decision

- **A node that asks per ADR skips a file too large to screen, whatever its `on_error`,** and the report lists the file as skipped. This is not a provider failure, so `docs/arch/0052-what-a-node-failure-does.md` doesn't apply to it, and a first node, which can't take `on_error: skip`, still skips the file.
- **Outside `--ci`, the skip counts as a failed file,** so a run left with no ADR check by it doesn't exit 0 (`docs/arch/0032-a-run-that-checked-nothing-fails.md`). Under `--ci` it is skipped as a file too large to judge is.
- **A node that asks per rule passes its candidates on, whatever `on_error` says.**

## Rules

- When a node that asks per ADR can't screen a file because the file is over `llm.max_tokens`, it MUST NOT keep or drop that file's candidates, and the file MUST be listed as skipped, naming the node.
- Outside `--ci`, that skip MUST count toward a run that checked nothing.
- When a node that asks per rule can't screen a file for the same reason, it MUST pass every candidate on.
- These rules cover only a file over `llm.max_tokens`. A provider failure, such as Jev timing out, still follows `on_error` and may pass candidates on.

## Alternatives considered

- **Pass every candidate on.** For a first node, that is one judgment per scope-matched ADR for every large file.
- **Keep nothing.** The file's violations go unreported, with nothing to show it was not checked.

## Consequences

- A large file in a pipeline that asks per ADR is checked only after `llm.max_tokens` is raised, or when it is judged as a diff.
