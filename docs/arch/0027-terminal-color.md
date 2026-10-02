---
title: Output color comes from line meaning and is decided per stream
status: Accepted
scope:
  - "internal/output/**"
  - "internal/cli/**"
---

# Output color comes from line meaning and is decided per stream

## Context

Violations, warnings and debug lines look the same in a terminal, so the line that matters is easy to miss. Color has to help a person at a terminal without changing a byte of what a pipe, a file, a CI log or the JSON report receives, and `check --format json` writes its document to stdout while its log goes to stderr, so one decision for the whole process would be wrong for one of the two streams.

## Decision

- **Meaning decides the color, inside `output.Printer`.** Each `Printer` method already says what a line is: `Warn` colors its label yellow, `Error` its label red, `Debug` dims the whole line, a `Group` header (a file) is bold cyan, a violation header bold red and a baselined one faint. Callers never pick colors. Every line opens and resets its own color, so no escape sequence spans a newline.
- **Color is off unless the `Printer` is built with `output.WithColor(true)`.** With it off, output is byte-identical to a build without color support.
- **Each stream decides for itself, once per run.** `cli.Execute` calls `output.ColorFor(mode, file)` for stdout and for stderr right after parsing, and every `Printer` over a stream uses that stream's answer, so under `--format json` a terminal stderr is colored while stdout carries plain JSON (the JSON document never goes through a `Printer`).
- **`--color=auto|always|never` on `check` and `index`.** `always` and `never` win over everything. `auto` asks `termenv`: the stream must be a terminal, `NO_COLOR` must be unset or empty, and `CI` must be unset; `CLICOLOR=0` turns color off and `CLICOLOR_FORCE` turns it on. ArchGuard adds `TERM=dumb`, which `termenv` ignores on Windows. An invalid value is a usage error (`2`) through kong's `enum`.
- **Windows consoles.** When color is on, `ColorFor` switches on the console's escape-sequence processing and returns a function that puts the previous mode back; `Execute` runs it on return, since the setting otherwise outlives the process and changes the user's shell. If switching it on fails, `auto` falls back to plain; `always` keeps color.
- Styling and detection use `github.com/muesli/termenv` rather than hand-written escape codes or terminal checks.

## Consequences

- CI logs stay plain by default, including under the GitHub Action, since runners set `CI`. A workflow that wants color passes `--color=always`.
- Git Bash/mintty presents a pipe rather than a console, so `auto` prints plain there; `--color=always` works.
- `init`'s prompts, the banner, `--version` and `main`'s final `Error:` line print through `fmt` (`docs/arch/0026-human-output-and-write-failures.md`) and stay uncolored.
- A new kind of line gets its color by getting its own `Printer` method or style, not by embedding escape codes in a message.
- `termenv`'s package initialization inspects stdout's environment and terminal status once on import; it writes nothing and reads nothing from the terminal.
