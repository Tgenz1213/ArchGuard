---
title: Human output goes through output.Printer; primary output fails the run
status: Accepted
scope:
  - "cmd/**"
  - "internal/**"
---

# Human output goes through output.Printer; primary output fails the run

## Context

ArchGuard prints two kinds of text: its primary output (help, the `index` summary, `check`'s result lines, the `--format json` document) and diagnostics (warnings, notes, progress dots, `[DEBUG]` lines). Written with ad-hoc `fmt.Fprintf` calls and discarded errors, a failed write of either kind is invisible, so a closed stdout in CI exits `0` and a pipeline reads it as a pass. Prefixes and indentation drift from call site to call site, and output from files analyzed in parallel has to be kept from interleaving at every call site.

## Decision

All human-readable output goes through `internal/output.Printer`, passed down from `internal/cli` rather than held in a package global:

- A nil `*Printer` writes to stderr, never stdout, so a component nobody wired up can't corrupt a JSON report.
- The `Printer` owns labels (`Warning:`, `Note:`, `Error:`, `[DEBUG]`) and indentation. `Group` buffers one file's block until `Flush`, `Indented` writes one level deeper, and `Violation` renders a violation. It locks around every write.
- **Primary output is fatal.** `Result` lines, `Violation` blocks, and any `Group` holding one are primary; `Err()` reports their first failed write. `cli` turns it into exit `1`, which takes precedence over `4`, `6` and `7` because the verdict never reached the reader; only an interrupt (`130`) wins over it. Help, usage and `--version` return their write errors to the same effect, and a failed write of the JSON document exits `1`.
- **Diagnostics are best-effort.** Every other `Printer` write ignores its error, so a broken stderr can't turn a clean check into a failure, which also protects the GitHub Action's exit codes.
- Under `--format json` the human lines are on stderr and only the JSON document is primary.
- `init`'s interactive prompts and the startup banner use `fmt` directly; kong prints help and usage (`docs/arch/0025-kong-command-line.md`).
- errcheck runs with `check-blank: true`. A discarded error is allowed only for cleanup after an error that already wins, or a call whose error can't occur or can't change the result, and each carries `//nolint:errcheck // <reason>` on the same line.

## Consequences

- New output uses the `Printer` of the component that prints it: `Result` for primary lines, `Info`/`Note`/`Warn`/`Error`/`Debug` for diagnostics.
- A `//nolint:errcheck` without the second `//` is not recognized and does not suppress anything, so `golangci-lint` flags it.
- On Unix, a broken pipe on stdout (`archguard check | head -1`) ends the process with SIGPIPE before any write error reaches ArchGuard, which still exits non-zero; Go's default SIGPIPE handling is kept for `| head` users.
- `NewEngine` leaves `Engine.Cache` unset; `cli` creates the cache and warns when it can't, so a run without a usable `.archguard/cache` proceeds uncached rather than failing.
