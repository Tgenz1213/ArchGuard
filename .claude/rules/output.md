---
paths:
  - "internal/output/**"
---

# `internal/output`

`Printer`, the one place that formats human-readable output: `Info`/`Note`/`Warn`/`Error`/`Debug`/`Field` add the label and indentation, `Group(header)` buffers a unit of work (a file) until `Flush` so parallel units print as contiguous blocks, `Progress` prints dots and ends the line before the next message, `Report` and `BaselineReport` render a check's end-of-run report (violations grouped by file, then the `Gaps`, then, for `Report` only, the `Coverage` line, then one summary line; the layout is asserted only in `internal/output`'s tests), and `Annotation` writes a best-effort GitHub Actions `::error` workflow command with its values escaped. It locks around every write, and a nil `*Printer` writes to stderr.

`Result` lines (every line of a report) and the violation groups of a report are primary output: `Err()` reports their first failed write, which `cli` turns into exit 1; every other write is best-effort (`docs/arch/0026-human-output-and-write-failures.md`). `internal/index`, `internal/analysis` and `internal/cli` print through it; only `init`'s prompts and status lines, the startup banner, `--version` and `main`'s final `Error:` line use `fmt` directly, and `kong` prints help and usage. `Indented()` is `Group`'s unbuffered sibling: one level deeper, written straight through, used where a header must print even with nothing under it (the `index` summary).

Color comes from each method's meaning (`Warn`/`Error` labels, `Debug` lines, `Group` file headers, violation headers) and is off unless the `Printer` is built with `WithColor(true)`; `cli.Execute` decides it once per stream with `output.ColorFor(mode, file)`, restoring a Windows console's mode on return, from `check`/`index`'s `--color` (`auto` asks `termenv`: a terminal, no `NO_COLOR`, no `CI`, and not `TERM=dumb`), so a `--format json` stdout is never colored. See `docs/arch/0027-terminal-color.md`.
