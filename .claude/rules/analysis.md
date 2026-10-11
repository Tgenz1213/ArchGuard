---
paths:
  - "internal/analysis/**"
  - "internal/cache/**"
---

# `internal/analysis`

The engine: `engine.go` (`Engine.Run`, context sizing), `run_state.go` (what a run collects across files) and `file_check.go` (one file's flow as `fileCheck` methods). `stage/` holds the scoring pipeline.

## A file's flow

`Engine.Run` checks files on a bounded worker pool (`analysis.max_concurrency`, default 5). For each file: fetch context, build candidates from `VectorStore.ScopedADRs` minus `archguard-ignore` suppressions, run the scoring stages, then ask the LLM about each surviving ADR. The LLM is the only source of violations; stages only choose which ADRs are judged.

- Which files are scanned is an `analysis.ContentProvider`, chosen by `cli`'s `checkCmd.contentProvider`.
- The scoring pipeline, its failure kinds and `on_error` are in `docs/arch/0022-candidate-scoring-pipeline.md`; its `--debug` lines in `docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md` and `docs/arch/0019-single-query-consistency-for-debug-diagnostics.md`.
- Context sizing counts tokens with the chat provider, prefers a diff to truncation, and truncates at a line boundary (`docs/arch/0002-provider-scoped-token-counting.md`). Under `--ci` a file that would be truncated is skipped with a warning instead (the CI Warn-Open policy) (`docs/arch/0028-check-report-and-log.md`).
- Under `--since` (`Engine.JudgeChange`) the model reads the file's diff and findings are checked against it (`docs/arch/0030-judge-the-change-under-since.md`).
- A cancelled context ends the run before any report or baseline (`docs/arch/0024-run-context-cancellation.md`).

## Findings and reports

All per-file log text goes through `Engine.Out`, one `Group` per file, so a file with nothing to report prints nothing and scorers' debug lines land under the file. `Engine` records what the reports are built from (violations, skipped and partly checked files, failed checks, stage failures, coverage), and `Engine.Report()` and `Engine.BaselineReport()` hand them to `output`. `Engine.checkFile` is where each file is counted in `Coverage`. See `docs/arch/0028-check-report-and-log.md` and `docs/arch/0014-json-check-output.md`.

## Cache

Judgment results are cached in `.archguard/cache/`, keyed by model, ADR content, file content and both prompts, so a hit skips the LLM call. `--suggest-fixes` suggestions use their own key and directory (`docs/arch/0016-llm-suggested-remediation.md`, `docs/arch/0017-suggestion-cache-key-namespace.md`).

## Footguns

- **`Engine.shouldExclude` always excludes `archguard-baseline.json`,** whatever `exclude_patterns` says, because the file quotes violating code (`docs/arch/0006-violation-baseline-file.md`).
