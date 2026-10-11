---
title: Machine-readable (--format json) output for archguard check
status: Accepted
scope: "internal/**"
---

# Machine-readable (--format json) output for archguard check

## Context

`archguard check` printed every violation as human-readable text on stdout. Nothing downstream, such as a Code Scanning integration, a dashboard or another script, could consume the results without scraping that text, although the structured data for each violation was already in memory. See #70.

## Decision

`archguard check` has `--format <text|json>`, with `text` as the default. With `--format json`:

- stdout carries exactly one JSON document, listing the violations and their count. The count is the same number that drives the drift exit code.
- Everything else meant for a person goes to stderr instead of being suppressed: the startup banner, `--debug` logging, progress, index-rebuild notices, and warnings the document has no field for. `--format json --debug` therefore still shows what happened without breaking a pipe that reads stdout.
- Other ADRs add their data to the same document: a remediation suggestion per violation (`docs/arch/0016-llm-suggested-remediation.md`), unverified findings that are not counted (`docs/arch/0031-unverified-findings-and-the-exit-code.md`), per-stage totals and stage failures (`docs/arch/0022-candidate-scoring-pipeline.md`), and token usage (`docs/arch/0034-token-usage-is-what-the-provider-reported.md`).
- Exit codes are unaffected: `--format` changes only what is printed, never what is returned.
- `--update-baseline` ignores `--format` and prints a note saying so. Its output is the baseline report (`docs/arch/0028-check-report-and-log.md`), a maintenance summary rather than the violation report this flag targets, and baselining suppresses the very violations the flag would report.

Human-readable output reaches stderr through an explicit writer, the `output.Printer`, passed to the code that prints. The rejected alternative was to silence stdout for the whole process, for example by swapping `os.Stdout` for the run. That is process-global, is not safe against the engine's concurrent workers, and would stop tests from asserting on stdout and stderr separately, which the e2e check that stdout is exactly one JSON document depends on.

## Consequences

- Any new human-readable output from `check` must go through the `Printer`, not a bare `fmt.Print*`, or it leaks onto stdout under `--format json` and breaks the safe-to-pipe guarantee.
- Test instrumentation in the e2e binary, such as a marker naming which mock provider ran, also writes to stderr, since that binary is held to the same stdout contract.
- `internal/index`'s progress and warnings follow the same rule through their own `Printer`; see `docs/arch/0015-index-diagnostic-writer.md`.
