---
title: check ends with a report on stdout; the run's log goes to stderr
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/cli/**"
  - "internal/output/**"
---

# check ends with a report on stdout; the run's log goes to stderr

## Context

`check` produces two kinds of text. One is the result a reader wants: what drifted, and what could not be checked. The other is the log of how the run went: the banner, progress, warnings, debug lines, and a line for each finding as it happens. When both share stdout, a saved or piped run holds its result buried in commentary, and a run that skipped files looks like a clean one unless the reader finds the warning. See #225.

## Decision

- **The result is on stdout, the log is on stderr.** In text mode `check` writes one report at the end of the run to stdout, and everything else to stderr, which is the split `--format json` has too (`docs/arch/0014-json-check-output.md`). A run whose analysis fails outright prints no report; its error is the result. `index` and `init` print on stdout.
- **The report is built from structured findings.** `Engine` records its violations, its baselined count and every gap in coverage, and `Engine.Report()` and `Engine.BaselineReport()` hand them to `output.Printer`. The `Printer` is the only definition of the layout and the only place it is asserted in tests; every other test asserts on the findings.
- **Gaps are listed and counted by kind.** `output.Gaps` holds files skipped (nothing was checked), files partly checked (analyzed through a truncated view), ADR checks that failed (the file was checked against its other ADRs), and stages that failed under `on_error: fail`. A file that was never read and a file that missed one rule are different gaps, so the summary counts each kind. A file too large to analyze in CI mode, where truncation warns instead of failing, is skipped.
- **The report says how much was checked.** `output.Coverage` counts the files judged (at least one ADR check returned a verdict), the ADR checks that returned a verdict (cached ones included), and the files with no relevant ADR (read in full, no candidate left after scoring). A clean run and a run that reached no ADR would otherwise print the same `0 new violation(s)`. The file counts are disjoint from the gap lists: a skipped or partly checked file is never judged, and a partly checked file is not counted as having no relevant ADR. A run that judged no file replaces the clean result with one that says so. The `--format json` document carries the same three counts in `coverage`, and the exit codes do not depend on them.
- **A drift exit prints no `Error:` line.** `runCheck` returns `ExitDriftDetected` with no error, so `main` does not repeat the report's result. Every other exit code keeps its error message.
- **`--output <path>` writes the report to a file** without color and with nothing on stdout. The destination is checked before the analysis starts, because `atomicfile.Write` creates missing directories and a typo should not cost a full run. The baseline file is refused as a destination, since the report would replace it and the next `check` could not load it. The report is saved atomically at the end, and a failed save exits `1`, which wins over the drift and stage-failure codes.
- **`--update-baseline` ends with a baseline report** under the same split, honoring `--output`. The baseline and its report are written together: an analysis failure, or a stage failure under `on_error: fail`, exits without either.

Alternatives considered:

- **A counts-only summary after the log.** It leaves the findings in the scrollback and gives a saved run nothing to read.
- **The report on stderr with the log.** It cannot be saved or piped apart from the log.
- **One "not fully checked" count for skipped files and failed checks.** It hides whether a file was never read or only missed a rule.
- **Printing a line per file in a clean run.** It is too noisy to read, and the counts answer the same question.
- **A non-zero exit when no file was judged.** It changes what a pipeline treats as failure, which is a policy decision of its own.
- **Writing the report to a file by default.** It adds a side effect nobody asked for.

## Consequences

- Anything that reads the text output from stdout sees only the report. Exit codes do not depend on which stream a line is on, and a terminal or a CI step log shows both streams.
- The report is `check`'s primary output (`docs/arch/0026-human-output-and-write-failures.md`). A failed write of it exits `1`; the log, including the line for each violation, is best-effort.
- A new kind of gap is a new field in `output.Gaps`, which both reports render and total.
- A count of what was checked is a field of `output.Coverage`. The baseline report does not render it.
