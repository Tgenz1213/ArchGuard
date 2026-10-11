---
title: Output color comes from line meaning and is decided per stream
status: Accepted
scope:
  - "internal/output/**"
  - "internal/cli/**"
---

# Output color comes from line meaning and is decided per stream

## Context

Violations, warnings and debug lines look the same in a terminal, so the line that matters is easy to miss. Color has to help a person at a terminal without changing a byte of what a pipe, a file, a CI log or the JSON report receives. `check` writes its result to stdout and its log to stderr (`docs/arch/0028-check-report-and-log.md`), so one decision for the whole process would be wrong for one of the two streams.

## Decision

- **Meaning decides the color, inside `output.Printer`.** Each `Printer` method already says what a line is (a warning, an error, a debug line, a file or violation header), so it picks the style. Callers never pick colors. Every line opens and resets its own color, so no escape sequence spans a newline.
- **Color is off unless the `Printer` is built with it on.** With it off, output is byte-identical to a build without color support.
- **Each stream decides for itself, once per run.** Every `Printer` over a stream uses that stream's answer. Under `--format json` a terminal stderr is colored while stdout carries plain JSON, since the JSON document never goes through a `Printer`. A report written with `--output` is never colored.
- **`--color=auto|always|never` on `check` and `index`.** `always` and `never` win over everything. `auto` asks `termenv`: the stream must be a terminal, `NO_COLOR` must be unset or empty, and `CI` must be unset; `CLICOLOR=0` turns color off and `CLICOLOR_FORCE` turns it on. ArchGuard adds `TERM=dumb`, which `termenv` ignores on Windows. An invalid value is a usage error (`2`).
- **Windows consoles.** When color is on, ArchGuard switches on the console's escape-sequence processing and puts the previous mode back on exit, since the setting otherwise outlives the process and changes the user's shell. If switching it on fails, `auto` falls back to plain; `always` keeps color.
- Styling and detection use `github.com/muesli/termenv` rather than hand-written escape codes or terminal checks.

## Consequences

- CI logs stay plain by default, including under the GitHub Action, since runners set `CI`. A workflow that wants color passes `--color=always`.
- Git Bash/mintty presents a pipe rather than a console, so `auto` prints plain there; `--color=always` works.
- `init`'s prompts, the banner, `--version` and `main`'s final `Error:` line print through `fmt` (`docs/arch/0026-human-output-and-write-failures.md`) and stay uncolored.
- A new kind of line gets its color by getting its own `Printer` method or style, not by embedding escape codes in a message.
- Importing `termenv` inspects stdout's environment and terminal status once; it writes nothing and reads nothing from the terminal.
