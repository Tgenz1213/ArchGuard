---
title: Machine-readable (--format json) output for archguard check
status: Accepted
scope: "internal/**"
---

# Machine-readable (--format json) output for archguard check

## Context

`Engine.Run` (`internal/analysis/engine.go`) formatted every violation directly into human-readable text and printed it straight to stdout. Nothing downstream (a Code Scanning integration, a dashboard, another script) could consume results without scraping that text, even though the structured data (ADR ID/title, file, line, reasoning, quoted code) already existed in memory per violation. See #70.

## Decision

`archguard check` gains `--format <text|json>` (default `text`, identical to the pre-existing output). With `--format json`:

- stdout carries exactly one JSON document: `{"violations": [...], "count": N}`, where each violation has `file`, `adr_id`, `adr_title`, `line`, `reasoning`, `quoted_code`, and (per `docs/arch/0016-llm-suggested-remediation.md`) an optional `suggestion`, and `count` matches `DriftDetectedError.Count` (the same number that drives the `ExitDriftDetected` exit code).
- Everything else that would normally print to stdout (the startup banner, `--debug` logging, per-file progress, index-rebuild notices, the final "No new architectural violations found." summary) is redirected to stderr instead of being suppressed outright, so `--format json --debug` still gives visibility into what happened without breaking a pipe consuming stdout.
- When a stage with `on_error: fail` fails (see `docs/arch/0022-candidate-scoring-pipeline.md`), the document also carries a `failures` array of `{stage, file, kind, error}`; it is omitted when empty.
- Exit codes are unaffected: `--format` only changes what's printed, never what's returned.
- `--update-baseline` ignores `--format` entirely (a printed note explains this) -- its output is a maintenance summary about the baseline file, not the violation report `--format json` targets, and baselining suppresses the very violations this flag would otherwise report.

Implementation-wise, `Engine` gained a `Writer io.Writer` field (defaulting to stdout) that `Info`/`Log` and all per-file progress output route through, plus a `JSONOutput bool` that -- alongside the existing text formatting -- also appends each new (non-baselined) violation to `CollectedViolations []Violation`, an exported struct mirroring the JSON shape. `cli.runCheck` points `Engine.Writer` at stderr when `--format json` is set, then marshals `CollectedViolations` to stdout after `Engine.Run` returns, regardless of whether it returned `DriftDetectedError` (previously the CLI returned immediately on that error, before any of the final summary printing).

The startup banner is printed unconditionally at the very top of `cli.Execute`, before subcommand flags are parsed, so it can't wait for `checkFlags.Parse` to learn the format. `Execute` peeks `os.Args` for `check ... --format json` (without `--update-baseline`) via `checkWantsJSON`, mirroring the value-flag handling `normalizePositionalArgPaths` already does for `--baseline-reason`, and skips the banner print when it matches. `format` was added to `valueFlagsBySubcommand` for the same reason `baseline-reason` is there: without it, `normalizePositionalArgPaths` would treat `json` in `--format json` as a positional file-path argument and mangle it via `filepath.Rel`.

An alternative considered: suppress stdout output at the `io.Writer` level for the whole process (e.g. swap `os.Stdout` briefly) rather than threading a `Writer`/`human` parameter through. Rejected because it's process-global and not concurrency-safe against `Engine.Run`'s worker pool, and because tests (`internal/cli`, e2e) need to assert on both streams independently -- an explicit writer parameter is what makes `TestE2E_CheckFormatJSON`'s "stdout is exactly one JSON document" assertion possible at all.

## Consequences

- Any future addition to `Engine`'s or `runCheck`'s human-readable output must go through `e.writer()` / the `human` writer, not a bare `fmt.Print*`, or it will leak onto stdout in `--format json` mode and break the "safe to pipe" guarantee.
- `cmd/archguard-e2e`'s mock provider factories printed a "which provider was invoked" marker via `fmt.Println` (test instrumentation, not production behavior) -- this leaked onto stdout even for the real CLI's stdout-purity contract as exercised through that test binary, and was moved to `os.Stderr`. Existing assertions using `CombinedOutput()` were unaffected since they read both streams merged.
- `--format` joins `baseline-reason` as a value-consuming flag `normalizePositionalArgPaths` and `checkWantsJSON` must both know about; a third such flag needs the same registration in `valueFlagsBySubcommand`, in both places, or path-normalization/banner-suppression will misfire on it.
- **Known gap**: `internal/index` (`BuildIndex`'s progress/warning output, `CompositeProvider.GetADRs`' provider-fetch warnings, `NewPgStore`'s pgvector-version warning) still writes directly to `os.Stdout`, independent of `runCheck`'s `human` writer. `runCheck` calls into these paths (an index-hash-mismatch rebuild, ADR fetching) on every `check`, so a `--format json` run that triggers a rebuild or a provider warning can still get non-JSON text ahead of the JSON document on stdout. This is out of scope here -- it requires threading a diagnostic writer through `internal/index`'s public API, a design decision of its own rather than a rider on this change -- and is tracked in #163. In the steady state (index already up to date, as the README's Quick Start already recommends running before `check`), this gap doesn't trigger.
