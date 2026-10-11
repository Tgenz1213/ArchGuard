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

A run shells out to git and calls an LLM provider for every file. A subprocess started without a context cannot be stopped, so the only way to end a run early is to kill the process. Cancelling cleanly has its own hazard: files the engine never reaches look like a clean pass, so a run cut short could exit `0`. Worse, an interrupted `--update-baseline` would write a partial scan over the committed baseline. See #213.

## Decision

- **One root context per run, cancelled by a signal.** Each `main` builds it with `cli.NotifyContext`, which cancels it on SIGINT or SIGTERM. After the first signal, default handling returns, so a second signal kills the process.
- **The context reaches every call that can block**, including every git call. A git call cut short returns the context's error, not git's exit status. `context.Background()` appears only at the root: the two `main`s and code with no caller context yet (#217, vector-store queries).
- **A cancelled run is a failed run, never a partial success.** The engine starts no new file once the context is done. It returns the context's error before building a summary or baseline snapshot, and `check` exits `ExitInterrupted` (130, the shell's 128 + SIGINT) without writing the baseline or the `--format json` report.
- **SIGTERM also exits 130**, not the conventional 143. Callers need one "stopped by a signal" code, not which signal it was.
- **Any command that fails after cancellation exits 130** with the error `interrupted`. This covers `index`, ADR fetching and repo-root discovery. A command that returns no error keeps its exit code even if a signal arrived afterward.
- **A late signal is not special-cased.** A signal that lands after `check` has printed its report still turns a drift (4) or stage-failure (6/7) exit into 130. The window is microseconds and the report is complete.
- **Lint enforces the subprocess half.** golangci-lint's `noctx` fails CI on an `exec.Command` or HTTP request without a context, tests included.

Rejected: passing `context.TODO()` at each call site to satisfy the linter. It silences the lint without making anything cancellable.

## Rules

- Every subprocess and HTTP request MUST be created with the caller's context (`exec.CommandContext`, `http.NewRequestWithContext`), never a fresh `context.Background()` or `context.TODO()` outside a `main` or test.
- A run cancelled before its analysis finishes MUST NOT exit `0`, print a summary or JSON report, or write the baseline file.

## Consequences

- Ctrl-C and CI timeouts stop a run within one in-flight call, exit `130`, and never leave a partial baseline.
- `analysis.ContentProvider`, `stage.File` and `cli.Execute` take a context, so new implementations and callers must thread one.
- An already-cancelled context ends a run before any file is read (`docs/standards/testing.md` covers skipping retry backoff in a test).
- Postgres queries through `index.VectorStore` are not cancellable until #217.
