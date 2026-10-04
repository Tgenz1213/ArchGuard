---
title: Machine-readable (--format json) output for archguard check
status: Accepted
scope: "internal/**"
---

# Machine-readable (--format json) output for archguard check

## Context

`Engine.Run` (`internal/analysis/engine.go`) formatted every violation directly into human-readable text and printed it straight to stdout. Nothing downstream (a Code Scanning integration, a dashboard, another script) could consume results without scraping that text, even though the structured data (ADR ID/title, file, line, reasoning, quoted code) already existed in memory per violation. See #70.

## Decision

`archguard check` has `--format <text|json>` (default `text`). With `--format json`:

- stdout carries exactly one JSON document: `{"violations": [...], "count": N}`, where each violation has `file`, `adr_id`, `adr_title`, `line`, `reasoning`, `quoted_code`, and (per `docs/arch/0016-llm-suggested-remediation.md`) an optional `suggestion`, and `count` matches `DriftDetectedError.Count` (the same number that drives the `ExitDriftDetected` exit code).
- Everything else that would normally print to stdout (the startup banner, `--debug` logging, per-file progress, index-rebuild notices, and a one-line warning when files were skipped or ADR checks failed, which the document has no field for) goes to stderr instead of being suppressed outright, so `--format json --debug` still gives visibility into what happened without breaking a pipe consuming stdout.
- The document also carries a `stages` array with one `{name, received, kept, duration_ms}` entry per pipeline stage, in order (see `docs/arch/0022-candidate-scoring-pipeline.md`).
- When a stage with `on_error: fail` fails (see `docs/arch/0022-candidate-scoring-pipeline.md`), the document also carries a `failures` array of `{stage, file, kind, error}`; it is omitted when empty.
- Exit codes are unaffected: `--format` only changes what's printed, never what's returned.
- `--update-baseline` ignores `--format` entirely (a printed note explains this) -- its output is the baseline report (`docs/arch/0028-check-report-and-log.md`), a maintenance summary about the baseline file, not the violation report `--format json` targets, and baselining suppresses the very violations this flag would otherwise report.

Implementation-wise, `Engine` prints its per-file log through its `Out *output.Printer` (a nil `Out` writes to stderr), and appends each new (non-baselined) violation to `CollectedViolations []Violation`, an exported struct mirroring the JSON shape; a `JSONOutput bool` makes it also collect the per-stage totals for the `stages` array. `cli.runCheck` builds `Out` over stderr in every format (`docs/arch/0028-check-report-and-log.md`), then marshals `CollectedViolations` to stdout after `Engine.Run` returns, whether or not it returned `DriftDetectedError`.

The startup banner is printed by `cli.Execute` right after the command line is parsed, to stderr for `check`, and skipped when `checkCmd.jsonOutput()` reports `--format json` without `--update-baseline` (see `docs/arch/0025-kong-command-line.md`).

An alternative considered: suppress stdout output at the `io.Writer` level for the whole process (e.g. swap `os.Stdout` briefly) rather than threading a `Writer`/`human` parameter through. Rejected because it's process-global and not concurrency-safe against `Engine.Run`'s worker pool, and because tests (`internal/cli`, e2e) need to assert on both streams independently -- an explicit writer parameter is what makes `TestE2E_CheckFormatJSON`'s "stdout is exactly one JSON document" assertion possible at all.

## Consequences

- Any future addition to `Engine`'s or `runCheck`'s human-readable output must go through the `Printer` / the `human` writer, not a bare `fmt.Print*`, or it will leak onto stdout in `--format json` mode and break the "safe to pipe" guarantee.
- `cmd/archguard-e2e`'s mock provider factories printed a "which provider was invoked" marker via `fmt.Println` (test instrumentation, not production behavior) -- this leaked onto stdout even for the real CLI's stdout-purity contract as exercised through that test binary, and was moved to `os.Stderr`. Existing assertions using `CombinedOutput()` were unaffected since they read both streams merged.
- `internal/index`'s progress and warnings (an index rebuild, ADR fetching) follow the same rule through their own `Printer`; see `docs/arch/0015-index-diagnostic-writer.md`.
