---
paths:
  - "internal/git/**"
---

# `internal/git`

Every function shells out to `git` with the caller's context and takes `ctx` first; a call cut short by cancellation returns the context's error, not git's exit status. See `docs/arch/0024-run-context-cancellation.md`.
