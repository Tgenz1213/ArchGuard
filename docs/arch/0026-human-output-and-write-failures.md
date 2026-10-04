---
title: Human output goes through output.Printer; primary output fails the run
status: Accepted
scope:
  - "cmd/**"
  - "internal/**"
---

# Human output goes through output.Printer; primary output fails the run

## Context

ArchGuard prints two kinds of text: its primary output (help, the `index` summary, `check`'s report, the `--format json` document) and diagnostics (warnings, notes, progress dots, `[DEBUG]` lines). Written with ad-hoc `fmt.Fprintf` calls and discarded errors, a failed write of either kind is invisible, so a closed stdout in CI exits `0` and a pipeline reads it as a pass. Prefixes and indentation drift from call site to call site, and output from files analyzed in parallel has to be kept from interleaving at every call site.

## Decision

All human-readable output goes through `internal/output.Printer`, passed down from `internal/cli` rather than held in a package global:

- A nil `*Printer` writes to stderr, never stdout, so a component nobody wired up can't corrupt a JSON report.
- The `Printer` owns labels (`Warning:`, `Note:`, `Error:`, `[DEBUG]`) and indentation. `Group` buffers one file's block until `Flush`, `Indented` writes one level deeper, and `Report` and `BaselineReport` render a check's end-of-run report (`docs/arch/0028-check-report-and-log.md`). It locks around every write.
- **Primary output is fatal.** `Result` lines, any `Group` holding one, and the groups of a report's violations are primary; `Err()` reports their first failed write. `cli` turns it into exit `1`, which takes precedence over every other exit code because the reader never got the output; only an interrupt (`130`) wins over it. Requested help and `--version` return their write errors to the same effect, and a failed write of the JSON document exits `1`. Usage printed because of a parse error is a diagnostic on stderr (`docs/arch/0025-kong-command-line.md`).
- **Diagnostics are best-effort.** Every other `Printer` write ignores its error, so a broken stderr can't turn a clean check into a failure, which also protects the GitHub Action's exit codes.
- In text mode `check`'s report is primary and on stdout, and its log, including the line for each violation, is on stderr. Under `--format json` the log is on stderr and only the JSON document is primary. With `--output` the report goes to a file, and a failed save exits `1`.
- `init`'s prompts and status lines, the startup banner, `--version` and `main`'s final `Error:` line use `fmt` directly; kong prints help and usage (`docs/arch/0025-kong-command-line.md`).
- errcheck runs with `check-blank: true`. A discarded error is allowed only for cleanup after an error that already wins, or a call whose error can't occur or can't change the result, and each carries `//nolint:errcheck // <reason>` on the same line.

## Consequences

- New output uses the `Printer` of the component that prints it: `Result` for primary lines, `Info`/`Note`/`Warn`/`Error`/`Debug` for diagnostics.
- A `//nolint:errcheck` without the second `//` is not recognized and does not suppress anything, so `golangci-lint` flags it.
- `cli.NotifyContext` ignores SIGPIPE, so on Unix a broken stdout or stderr pipe returns `EPIPE` and follows the same rules: `archguard check | head -1` exits `1` with `failed to write output: broken pipe`, and a broken stderr pipe changes nothing.
- When a failed write overrides the exit code, the command's own error is kept in the message alongside the write error.
- `NewEngine` leaves `Engine.Cache` unset; `cli` creates the cache and warns when it can't, so a run without a usable `.archguard/cache` proceeds uncached rather than failing.
