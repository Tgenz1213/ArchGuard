---
title: Command line parsed with kong
status: Accepted
scope:
  - "cmd/**"
  - "internal/cli/**"
---

# Command line parsed with kong

## Context

A CLI hand-built on the standard `flag` package keeps its help and usage as literal text that drifts from the real flags. `flag` also parses a subcommand's flags only once that subcommand runs, so anything needed earlier (the startup banner's JSON check, repo-relative file paths) has to scan raw `os.Args` and know by hand which flags consume the next argument. And it stops at the first positional argument, so `archguard check a.go --debug` would treat `--debug` as a file.

## Decision

The command line is declared once as structs in `internal/cli/commands.go` (`commandLine`, `checkCmd`) and parsed with `github.com/alecthomas/kong`, which generates help from the same declarations.

- `parseCommandLine` runs first in `cli.Execute`, before git discovery or config loading, so `--help`, `help`, `<command> --help` and `--version` work outside a repository. It returns a nil invocation when parsing already answered the command line.
- Exit codes stay ArchGuard's: `kong.Exit` is overridden, so help and `--version` exit `0` through `Execute`'s return, a parse error prints the relevant usage and exits `2` (usage), and a failed write of help, usage or the version exits `1`, per the primary-output rule.
- The banner decision and path normalization read the parsed values (`checkCmd.jsonOutput`, `normalizePaths(checkCmd.Paths, ...)`) instead of scanning `os.Args`, so no flag needs registering anywhere else.
- `--version` is a `kong` flag printing `cli.Version`, which `cmd/archguard` sets from its build-time variables, so any binary built on `cli.Execute` handles it.
- `help` as a first argument is rewritten to `--help`, keeping the older spelling working.
- kong was chosen over cobra for its struct-tag declarations, generated help in this layout, and a standard-library-only core.

## Consequences

- Flags use POSIX syntax only: `--debug` and `--format json`/`--format=json`. Go's single-dash long form (`-debug`) is rejected as a usage error; `action.yml` already uses `--ci`.
- Flags may follow file arguments (`archguard check a.go --debug`).
- A new command or flag is a struct field with `help` and, for flags, `name`/`enum`/`default` tags; its help text and validation come with it. An invalid `--format` is rejected by kong's `enum` before `runCheck` runs.
- `-v, --version` appears in every command's help, since kong lists global flags with each command's own.
