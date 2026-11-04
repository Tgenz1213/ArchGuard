---
paths:
  - "internal/analysis/**"
  - "internal/cache/**"
---

# `internal/analysis`

- **Files are checked concurrently, so print about a file only through its `fileCheck.out` group.** The group prints as one block when the file is done; a line written to the run's printer instead interleaves with other files.
- **`Engine.shouldExclude` always excludes `archguard-baseline.json`,** whatever `exclude_patterns` says, because the file quotes violating code (`docs/arch/0006-violation-baseline-file.md`).
- **Anything new that can change the model's answer goes into `cache.AnalysisKeyInput` or `cache.SuggestionKeyInput`,** or a cached answer to a different question is reused.
- **A scorer that answers without a quote reads `node.File.Sides()`, never the text the LLM judges,** so a diff reaches it as a before side and an after side with no `+`/`-` markers (`docs/arch/0053-quoteless-runners-read-both-sides-of-a-diff.md`).
- **What `BuildPipeline` hands a node, such as `Askers`, is an object with methods, not a bare func** (`docs/arch/0049-pipeline-nodes.md`).
