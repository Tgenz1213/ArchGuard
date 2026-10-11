---
paths:
  - "internal/analysis/**"
  - "internal/cache/**"
---

# `internal/analysis`

- **Files are checked concurrently, so print about a file only through its `fileCheck.out` group.** The group prints as one block when the file is done; a line written to the run's printer instead interleaves with other files.
- **`Engine.shouldExclude` always excludes `archguard-baseline.json`,** whatever `exclude_patterns` says, because the file quotes violating code (`docs/arch/0006-violation-baseline-file.md`).
- **Anything new that can change the model's answer goes into `cache.AnalysisKeyInput` or `cache.SuggestionKeyInput`,** or a cached answer to a different question is reused.
