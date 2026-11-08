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

- **A node that asks per ADR skips a file too large to screen, and the report lists it as skipped.** This is not a provider failure, so the pass-on rule of `docs/arch/0052-what-a-node-failure-does.md` doesn't apply to it.
- **Under `on_error: fail`, or while `--update-baseline` runs, it is a node failure whose kind is a precondition not met,** since a skipped file's entries would drop out of a new baseline.
- **A node that asks per rule passes its candidates on, whatever `on_error` says.**

## Rules

- A node that asks per ADR MUST NOT keep or drop candidates for a file over `llm.max_tokens` that it hasn't screened.
- Under `on_error: skip` without `--update-baseline`, that file MUST be listed as skipped.
- Under `on_error: fail`, or while `--update-baseline` runs, that file MUST be a precondition-not-met node failure.

## Alternatives considered

- **Pass every candidate on.** For a first node, that is one judgment per scope-matched ADR for every large file.
- **Keep nothing.** The file's violations go unreported, with nothing to show it was not checked.

## Consequences

- A large file in a pipeline that asks per ADR is checked only after `llm.max_tokens` is raised, or when it is judged as a diff.
