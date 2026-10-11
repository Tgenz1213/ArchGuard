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

- **All human-readable output goes through `internal/output.Printer`**, passed down from `internal/cli` rather than held in a package global.
- **A nil `*Printer` writes to stderr, never stdout**, so a component nobody wired up can't corrupt a JSON report.
- **The `Printer` owns labels and indentation**, and keeps each file's block whole so parallel files do not interleave. It renders `check`'s end-of-run report (`docs/arch/0028-check-report-and-log.md`).
- **Primary output is fatal.** A failed write of it exits `1`, which takes precedence over every other exit code because the reader never got the output; only an interrupt (`130`) wins over it. This covers requested help, `--version` and the JSON document. Usage printed because of a parse error is a diagnostic on stderr (`docs/arch/0025-kong-command-line.md`).
- **Diagnostics are best-effort.** A failed write of one is ignored, so a broken stderr can't turn a clean check into a failure. This also protects the GitHub Action's exit codes.
- **Which output is primary depends on the mode.** In text mode `check`'s report is primary and on stdout, and its log, including the line for each violation, is on stderr. Under `--format json` the log is on stderr and only the JSON document is primary. With `--output` the report goes to a file, and a failed save exits `1`.
- **A few lines bypass the `Printer`.** `init`'s prompts and status lines, the startup banner, `--version` and `main`'s final `Error:` line use `fmt` directly; kong prints help and usage (`docs/arch/0025-kong-command-line.md`).

## Rules

- Human-readable output MUST go through `internal/output.Printer`, except `init`'s prompts and status lines, the startup banner, `--version`, `main`'s final `Error:` line, and kong's help and usage.
- A `Printer` MUST be passed down from `internal/cli`, not held in a package global.
- A nil `*Printer` MUST write to stderr, never stdout.
- A failed write of primary output MUST exit `1`, unless the run was interrupted.
- A failed write of a diagnostic MUST be ignored.

## Alternatives considered

None recorded.

## Consequences

- New output uses the `Printer` of the component that prints it: `Result` for primary lines, the diagnostic methods for the rest.
- SIGPIPE is ignored, so on Unix a broken stdout or stderr pipe returns `EPIPE` and follows the same rules: `archguard check | head -1` exits `1` with `failed to write output: broken pipe`, and a broken stderr pipe changes nothing.
- When a failed write overrides the exit code, the command's own error is kept in the message alongside the write error.
- A run without a usable `.archguard/cache` warns and proceeds uncached rather than failing.
