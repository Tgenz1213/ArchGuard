---
paths:
  - "internal/cli/**"
  - "cmd/archguard/**"
---

# `internal/cli`

One file per job: `cli.go` (`Execute`, startup), `commands.go` (the `kong` command line), `validate.go`, `providers.go`, `streams.go` (per-stream color and printers), `adr_provider.go` (shared by `check` and `index`), and one file per command (`init.go`, `index.go`, `check.go` with `check_index.go`).

## Startup

`Execute(ctx, version, factories)` parses the command line before touching git or config, so help and `--version` work outside a repository (`docs/arch/0025-kong-command-line.md`). It then `chdir`s to the git root (every path is repo-relative), loads `archguard.yaml`, validates it, builds the providers and dispatches. `factories` (`ProviderFactories{Chat, Embed}`) is the test injection point and is zero in production.

Provider pairing, the rules `validateProviderConfig` enforces, and why `ARCHGUARD_EMBEDDING_API_KEY` never falls back to `ARCHGUARD_API_KEY` are in `docs/arch/0004-decoupled-chat-and-embedding-providers.md`. One instance serves both roles when `llm.provider` and `vector_store.provider` resolve to the same name.

## Output streams

`check` writes its result to stdout and everything else to stderr: the end-of-run report in text mode, or the one JSON document under `--format json` (`docs/arch/0028-check-report-and-log.md`, `docs/arch/0014-json-check-output.md`). `checkRun` holds two printers, `log` (stderr) and `report` (stdout, or a buffer saved to `--output`), and passes `log` to everything it calls that prints, including an index rebuild. `index` and `init` print on stdout.

With `GITHUB_ACTIONS=true` and text output, `runCheck` prints one `::error` annotation per new violation after the run, but not under `--update-baseline`.

## Exit codes

Exit codes are meaningful and tested; preserve them if you touch command dispatch.

| Code | Meaning |
|---|---|
| `0` | Success, including files skipped under the default `on_error: skip` |
| `1` | General error, including a failed write of primary output, which wins over every code but `130` |
| `2` | Usage |
| `3` | Config |
| `4` | Drift detected, returned with a nil error so `main` prints no `Error:` line |
| `5` | Index error, including zero valid ADRs (`docs/arch/0010-index-corpus-health-reporting.md`) |
| `6` | A stage with `on_error: fail` hit an unavailable dependency |
| `7` | A stage with `on_error: fail` hit an unmet precondition; wins over `6`, and both win over `4` |
| `130` | Interrupted by SIGINT or SIGTERM (`docs/arch/0024-run-context-cancellation.md`) |

## Footguns

- **`Execute` rewrites `check`'s positional paths relative to the repo root before it `chdir`s.** A new command with positional paths needs the same `normalizePaths` call, and a positional that isn't a path (like `--since`'s ref) must stay out of it. A new path-valued flag needs the same treatment as `--output` in `checkCmd.resolvePaths`.
- **`.` anywhere in `check`'s paths means the whole repo.** `checkCmd.contentProvider` checks `slices.Contains(c.Paths, ".")`, so `check foo.go .` scans everything and prints a note naming the other paths.
- **`--output` is validated at the top of `runCheck`** (`checkReportDestination`), because `atomicfile.Write` creates missing directories and a typo should not cost a full run.
- **`runIndexCommand` is the only entry point for `archguard index`.** `check`'s rebuild on a hash mismatch (`checkRun.rebuildIndex`) calls `runIndex` directly with `check`'s `log` printer.
