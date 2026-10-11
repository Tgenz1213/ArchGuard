---
paths:
  - "internal/git/**"
---

# `internal/git`

All git plumbing (repo root discovery, staged/uncommitted/tracked file lists, diffs) shells out to `git` via `exec.CommandContext` with the caller's context, so cancelling a run kills in-flight git processes; every exported function takes `ctx` first, and so do the `analysis.ContentProvider` methods and `stage.File.QueryText` that call them. A git call that fails because its context was cancelled returns the context's error (`errors.Is(err, context.Canceled)`), not git's exit status. See `docs/arch/0024-run-context-cancellation.md`.
