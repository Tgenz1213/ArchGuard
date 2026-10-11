---
title: A run that errors left with no ADR check fails with exit code 8
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/cli/**"
---

# A run that errors left with no ADR check fails with exit code 8

## Context

A file is skipped when it cannot be read, when its candidate ADRs cannot be loaded, or when a scoring stage with `on_error: skip` fails, and an ADR check fails when the LLM call fails. Each is listed in the report, but none changed the exit code. A run where every one of them failed, because the database, the embedding provider or the LLM was down, exited `0` like a clean run, so a pipeline gating on the exit code could stop checking anything and stay green. See #127.

## Decision

- **A run exits `8` when errors left it with no ADR check.** That is: at least one file was skipped by an error or at least one ADR check failed, and no ADR check returned a verdict. It applies to every mode, `--ci` or not, and to every skip cause alike.
- **Partial failures do not change the exit code.** When any ADR check returned a verdict, skipped files and failed checks are listed in the report. That is what `on_error: skip` is for.
- **A file skipped as too large under `--ci` is not an error.** It is the Warn-Open policy (`docs/arch/0028-check-report-and-log.md`), so it alone never makes a run exit `8`.
- **The report is still written.** `--update-baseline` does not write the baseline, because a baseline from a run that checked nothing would drop every entry.
- **Precedence.** A failed write of the report and a stage failure under `on_error: fail` win over `8`. A run that exits `8` found no violation, so it never competes with `4`.

## Rules

- Code that lets a check run in which errors left no ADR check exit `0`.

## Alternatives considered

- **Fail on any skipped file.** One flaky embedding call would fail a pull request, and `on_error: skip` would mean nothing.
- **Fail only under `--ci`.** A pre-commit hook would pass while its provider is down, which is the same silent pass on a developer's machine.
- **Exit `1`.** A pipeline could not tell "nothing was checked" from a crash or a bad command line.
- **Reuse `6`.** It means a stage set to `on_error: fail` failed, which a reader of the config would not expect from a stage left on `skip`.
- **Fail when no file was judged.** A run where every candidate scored below the threshold judges nothing without any error; that is a configuration problem the report already explains (#280), not an outage.

## Consequences

- An outage of the database, the embedding provider or the LLM fails the run instead of passing it.
- A path that does not exist, as in `check missing.js`, exits `8`.
- A run with one file skipped by an error and every other file without a relevant ADR exits `8`, since no ADR check ran.
