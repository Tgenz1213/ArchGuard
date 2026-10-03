---
title: Run context and signal cancellation
status: Accepted
scope:
  - "cmd/**"
  - "internal/cli/**"
  - "internal/analysis/**"
  - "internal/git/**"
  - "internal/inference/**"
  - "internal/index/**"
---

# Run context and signal cancellation

## Context

A run shells out to git and calls LLM providers for every file. A subprocess started without a context can't be stopped, so the only way to end a run early would be to kill the process outright. Cancelling cleanly raises its own hazard: files the engine never reaches look like a clean pass, so a run cut short could exit `0`. Worse, an interrupted `--update-baseline` would snapshot a partial scan over the committed baseline. See #213.

## Decision

Each `main` builds the run's root context with `cli.NotifyContext`, which cancels it on SIGINT or SIGTERM. Once cancelled, it restores default signal handling, so a second signal kills the process. The context flows through `cli.Execute(ctx, ...)` into `Engine.Run`, the `analysis.ContentProvider` methods, `stage.File.QueryText`, and every `internal/git` function. Those functions run git with `exec.CommandContext`, and a git call cut short by cancellation returns the context's error, not git's exit status. `context.Background()` appears only at the root: the two `main`s and code with no caller context yet (#217, vector-store queries).

A cancelled run is a failed run, never a partial success:

- `Engine.Run` skips every file it hasn't started once the context is done, and returns the context's error before building any summary or baseline snapshot.
- `runCheck` returns `ExitInterrupted` (130, the shell's 128 + SIGINT) before writing the baseline or the `--format json` report. SIGTERM also exits 130 rather than the conventional 143: callers need one "stopped by a signal" code, not which signal it was.
- `cli.Execute` maps any error returned while the context is done to `ExitInterrupted` with the error `interrupted`, which covers `index`, ADR fetching, and repo-root discovery. A command that returns no error keeps its exit code even if a signal arrived afterward. A signal landing after `runCheck` has already printed its report still turns a drift (4) or stage-failure (6/7) exit into 130; the window is microseconds and the report is complete, so this is accepted rather than special-cased.

golangci-lint's `noctx` enforces the subprocess half: an `exec.Command` or HTTP request without a context fails CI, tests included (tests use `t.Context()`).

Rejected: passing `context.TODO()` at each call site to satisfy the linter. It silences the lint without making anything cancellable.

## Rules

- Every subprocess and HTTP request MUST be created with the caller's context (`exec.CommandContext`, `http.NewRequestWithContext`), never a fresh `context.Background()` or `context.TODO()` outside a `main` or test.
- A run cancelled before its analysis finishes MUST NOT exit `0`, print a summary or JSON report, or write the baseline file.

## Consequences

- Ctrl-C and CI timeouts stop a run within one in-flight call, exit `130`, and never leave a partial baseline.
- `analysis.ContentProvider`, `stage.File` and `cli.Execute` take a context, so new implementations and callers must thread one.
- An already-cancelled context ends a run before any file is read, so a test that needs to skip LLM retry backoff returns a `backoff.Permanent` error instead.
- Postgres queries through `index.VectorStore` are not yet cancellable (#217).
