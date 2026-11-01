---
title: What a node failure does
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/cli/**"
  - "internal/config/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# What a node failure does

## Context

A node that chooses which ADRs go on (`docs/arch/0049-pipeline-nodes.md`) calls a provider that can be down, slow or refuse the key. What happens then decides whether an outage costs money, hides a problem, or fails the run.

## Decision

- **A choosing node's `on_error` is `skip` or `fail`, default `skip`.** Judging and suggesting nodes take no `on_error`.
- **Under `skip`, a node that ranks by similarity skips the file; any other choosing node passes its ADRs on unchanged** and never drops them because its provider failed.
- **A first node that would pass its ADRs on can't skip,** since that would send every scope-matched ADR to the judge; it defaults to `fail`.
- **Under `fail`, that file's later nodes stop and other files still run.** The failure's kind, a dependency that didn't respond or a precondition that isn't met, picks the exit code, which outranks the drift exit code, and `--update-baseline` writes nothing.
- **A node that would pass its ADRs on stops the check with exit 3 when its provider rejects the key,** whatever `on_error` says. A revoked key is a config error, not an outage to pay for on every file.

## Rules

- Pipeline validation MUST reject `on_error: skip` on a first node that would pass its ADRs on when it fails.
- A node that would pass its ADRs on and fails under `on_error: skip` MUST pass them on unchanged, unless its key was rejected or the run was cancelled.
- A node that ranks by similarity and fails under `on_error: skip` MUST skip the file, unless the run was cancelled.
- A node failure under `on_error: fail` MUST pick the exit code by its kind, MUST take precedence over the drift exit code, and MUST stop `--update-baseline` writing the baseline.
- A node that would pass its ADRs on MUST fail the check with exit code 3 when its provider rejects its key, whatever `on_error` says.

## Alternatives considered

- **Pass on everything on any failure.** An outage at the first node would make one judge call per scope-matched ADR per file.
- **Fail on every failure.** One slow call would fail a pull request, and `skip` would mean nothing.

## Consequences

- While a pass-on node's provider is down under `skip`, the check costs more but misses nothing.
- While a similarity ranker's provider is down under `skip`, files are skipped.
