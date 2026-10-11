---
paths:
  - "internal/cli/**"
  - "cmd/archguard/**"
---

# `internal/cli`

One file per job: `cli.go` (`Execute`, startup, `runSetup`), `validate.go` (config validation), `providers.go` (provider construction), `init.go`, `check.go` (plus `check_index.go`, its index load and rebuild) and `index.go` (the commands), `adr_provider.go` (the ADR provider `check` and `index` both build), `streams.go` (stdout/stderr color and printers), `commands.go` (the `kong` command line).

## Startup

Parses the command line with `kong` (`commandLine`/`checkCmd` in `commands.go`, help generated from them) before touching git or config, so help and `--version` work anywhere (see `docs/arch/0025-kong-command-line.md`), then finds the git root and `chdir`s into it (all paths are resolved relative to repo root, not cwd), loads `archguard.yaml` via `internal/config`, constructs the chat provider (`llm.provider`) and embedding provider (`vector_store.provider`), and dispatches to `init` / `index` / `check`.

`Execute(ctx, version, factories)` takes the build's version string (stamped by ldflags in `cmd/archguard`, handed to kong for `--version`), the run's root context, which every git subprocess and LLM provider call hangs off (`VectorStore` methods other than `BuildIndex` take none, so a Postgres query isn't cancellable), and a `ProviderFactories{Chat, Embed}` struct as its test injection point (zero value in production; `Chat` returns an `inference.Provider` because its instance also embeds when the two provider names match, `Embed` an `inference.Embedder`).

## Providers

`resolveEmbedProviderInstance` reuses the chat provider for embedding when the two roles share a provider name; otherwise it calls `factories.Embed`, or fails fast at `ExitConfig` if `factories.Embed` is nil — the same fail-fast contract the real-provider path has via its builders' error returns. On the real-provider path (`buildProviders`), matching names get one `buildProvider` instance (openai, ollama, gemini) for both roles; otherwise `buildChatProvider` (claude, or `buildProvider`) and `buildEmbedProvider` (voyage, or `buildProvider`) build each role separately. `Execute` passes one instance to the engine for both roles when `vector_store.provider` resolves to `llm.provider`, and separate ones otherwise (required when `llm.provider` is `claude`, since Claude has no embeddings API — see `docs/arch/0004-decoupled-chat-and-embedding-providers.md`).

- **`llm.provider: claude` requires `vector_store.provider` to be set explicitly, `vector_store.provider: claude` is rejected outright, and `llm.provider: voyage` is rejected outright.** Claude has no embeddings API, so there's no safe default to fall back to (falling back to `llm.provider` would mean falling back to Claude itself) and it can never be `vector_store.provider` either. Voyage has no chat API at all, so it can never be `llm.provider`, regardless of `vector_store.provider`. `internal/cli.validateProviderConfig` catches all three at config load (`ExitConfig`); see `docs/arch/0004-decoupled-chat-and-embedding-providers.md`.
- **`ARCHGUARD_EMBEDDING_API_KEY` does not fall back to `ARCHGUARD_API_KEY`.** When `vector_store.provider` differs from `llm.provider`, the embedding provider is a different vendor by construction, so `internal/cli.Execute` builds it with `ARCHGUARD_EMBEDDING_API_KEY` only — an unset `ARCHGUARD_EMBEDDING_API_KEY` produces the standard "no API key set" warning and likely-failing requests, not a silent reuse of the chat provider's key against the wrong vendor's API.

## `check` output

`check` writes its result to stdout and everything else (banner, progress, warnings, debug, index-rebuild notices, one line per violation) to stderr: in text mode the result is the end-of-run report, and `--format json` (default `text`) prints a single JSON document there instead (`docs/arch/0028-check-report-and-log.md`, and `docs/arch/0014-json-check-output.md` for the `Engine.Out`/`JSONOutput` plumbing). `runCheck` builds a `checkRun` that holds its two printers, `log` (stderr) and `report` (stdout, or a buffer saved to `--output` at the end); the startup banner goes to stderr for `check` and is skipped when the parsed `checkCmd.jsonOutput()` is true. A drift exit returns `ExitDriftDetected` with a nil error, so `main` prints no `Error:` line repeating the report; a run whose analysis fails outright prints no report. In text output with `GITHUB_ACTIONS=true`, `runCheck` prints one `::error` annotation per new violation after `Engine.Run` returns, never under `--format json` or `--update-baseline`; a violation whose line isn't a line of the file (diff mode, unverified quote) is annotated on the file.

`checkRun` hands its stderr `log` printer to `cli.runIndex` (which takes an `*output.Printer`; `archguard index` passes one over `os.Stdout`) so a `check` stdout that carries only the report or the JSON document stays clean even when it triggers an index rebuild or a provider-fetch warning. See `docs/arch/0015-index-diagnostic-writer.md`.

`checkCmd.contentProvider` picks the `analysis.ContentProvider`, and `--since` is a git ref, not a path, so it stays out of `normalizePaths`.

## Exit codes

Exit codes are meaningful and tested: `0` success, `1` general error (including a failed write of primary output, which wins over every other code except `130`), `2` usage, `3` config, `4` drift detected (returned with a nil error, so no `Error:` line), `5` index error, `6` a stage with `on_error: fail` hit an unavailable dependency, `7` a stage with `on_error: fail` hit an unmet precondition (the default `on_error: skip` skips the file and exits `0`), `130` interrupted by SIGINT/SIGTERM (`cli.NotifyContext` cancels the run's context; `cli.Execute` maps any error returned while it's done to `ExitInterrupted`, and a cancelled `Engine.Run` returns the context error before any report or baseline write — see `docs/arch/0024-run-context-cancellation.md`) — preserve these in `internal/cli` if you touch command dispatch.

## Footguns

- **`cli.Execute` rewrites `check`'s parsed paths relative to the repo root, then `chdir`s there.** `normalizePaths` runs on `checkCmd.Paths` after parsing, so a new command with its own positional paths needs the same call, and a non-path positional argument must not be passed to it. Help and `--version` return from `parseCommandLine` before git discovery or config loading.
- **`runIndexCommand` is the only CLI entry point for `index`** (it builds the stdout printer and turns a failed summary write into exit 1). `checkRun.rebuildIndex`, `check`'s auto-rebuild on a hash mismatch, calls `runIndex` directly with `check`'s own printer, so the rebuild's output goes to `check`'s stderr log.
- **`checkCmd.contentProvider` treats `.` as a whole-repo-scan request anywhere in the positional args, not just first.** It checks `slices.Contains(c.Paths, ".")`, so `archguard check foo.go .` and `archguard check . foo.go` both trigger `AllProvider`, with a note naming every other path argument regardless of `.`'s position. Flags may come before or after file arguments, and only the POSIX `--flag` form is accepted: a single-dash `-debug` is a usage error (exit 2).
- **`check --output` is resolved and validated in `cli`, not by `atomicfile`.** `checkCmd.resolvePaths` makes a relative `--output` absolute against the starting directory before `execute` changes into the repo root (a new path-valued flag needs the same call), and `checkReportDestination` rejects a missing directory, a parent that is a file, a target that is a directory, or the baseline file (`isBaselineFile`, which would otherwise be overwritten by the report) at the top of `runCheck`, because `atomicfile.Write` would create missing directories and a typo should not cost a full run. A failed save returns `ExitError` joined ahead of the drift and stage-failure codes. The report is written only when it was produced: not on an interrupt, an early config error, or a non-drift analysis failure in text mode.
