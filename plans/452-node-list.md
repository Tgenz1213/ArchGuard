# Plan for #452: `analysis.pipeline` as a list of nodes

Status: **draft, under review.** Not posted to the ticket; no code until Tim approves.

Stacked on PR #464 (#451's ADRs). Builds against ADRs 0049 to 0054.

## Scope

Three node kinds only: `rank` (cosine), `ask` (Jev, `noul`, `for_each: rule`), `judge` (the `llm:` block). Everything else in the node table arrives with its own ticket.

## Config shape (#452 only)

```yaml
jev:
  model: <optional; the client's default otherwise>

analysis:
  pipeline:
    - name: shortlist
      job: rank
      with: cosine
      threshold: 0.6     # optional; falls back to vector_store.similarity_threshold
      top_k: 5           # optional; falls back to analysis.max_relevant_adrs
      on_error: skip     # optional

    - name: breaks-a-rule
      job: ask
      with: jev
      preset: breaks-a-rule        # or type: noul, for_each: rule, instructions: "...{rule}..."
      keep:
        threshold: 0.3             # the preset's default; required without a preset
        top_k: 3                   # optional
      on_error: skip

    - name: judge
      job: judge
      with: llm
```

Allowed keys per node: `rank` takes `name`, `job`, `with`, `threshold`, `top_k`, `on_error`. `ask` takes `name`, `job`, `with`, `preset`, `type`, `for_each`, `instructions`, `keep`, `on_error`. `judge` takes `name`, `job`, `with`. Anything else is "a setting not allowed on the node".

## Changes

Each commit builds and passes its own tests (review H1).

1. **Pipeline config and its builder, in one commit with their unit tests** (ADR 0051).
   - `config.Analysis.Pipeline` becomes the raw `yaml.Node`; `LoadConfig` never inspects it, nor the new top-level `jev:` block, so `archguard index` loads any pipeline.
   - New `config.ParsePipeline(cfg) ([]Node, error)`. A missing or null pipeline, or a mapping (the old slots), gets the "run `archguard init`" message with the default pipeline to paste. `pipeline: []` fails the "exactly one judge" rule. Then validation rules 1-8 for these three kinds, each error naming the node.
   - Node names: required, unique, letters, digits, `-` and `_`, at most 64 characters (they go to the terminal and JSON).
   - A second `rank` is allowed; every `rank` falls back to `vector_store.similarity_threshold` and `analysis.max_relevant_adrs` (the `rerank` defaults of 0.0 and 3 go).
   - `ask` takes `model` too (the design's node table), overriding `jev.model`. `jev:` takes only `model` here; `max_tokens` and `timeout` come with the tickets that use them.
   - Presets: `breaks-a-rule` only (`type: noul`, `for_each: rule`, `instructions: "Does this code break the following rule? {rule}"`, `keep.threshold: 0.3`). `type`, `for_each`, `instructions` or `criteria` beside a preset is an error.
   - Placeholders are `{name}` with `name` matching `[a-z_]+`; other braces are literal text. With `for_each: rule`, `{rule}` is required and may repeat; `{code}`, `{adr}` and unknown names are errors. `for_each: adr`, `choice` and `score` are rejected as not supported yet.
   - `BuildStages` becomes `BuildPipeline(cfg, nodes, ...)`; a stage's name is its node's name. `Engine.scoringStages`' built-in cosine fallback is removed (ADR 0051); tests build their stages explicitly.
   - `JevScreen` builds questions from the node's `instructions`, replacing `{rule}` with the rule's statement and examples exactly as `questionText` does today; with no instructions set it uses the preset's, so the existing screening tests pass unchanged.
   - A test pins the stable tie order at the top-K cut.
   - Removed: `Pipeline`, `StageConfig`, `JevOptions`, `decodeStage`, the scorer name lists.
2. **`check` requires the pipeline, plus every config that needs one, in one commit.**
   - `runCheck` calls `ParsePipeline` first, before the vector store or index load, so a bad pipeline exits 3 without touching the database or paying for a rebuild (review H2). The Jev screener and key are built there too; `loadSetup`, which `index` shares, no longer reads them.
   - Every e2e config gains the default pipeline; the Jev e2e tests move to the list shape. Their expected findings and exit codes stay the same, which is the "same as today's no-pipeline run" check.
   - `archguard.yaml`: `shortlist` rank, `breaks-a-rule` ask (today's `pre_judge` at 0.3), `judge`. `.github/archguard-ci.yaml`: the default pipeline (CI reindexes once, since its hash covers that file).
   - e2e: `index` succeeds on a config `check` rejects.
3. **Both sides of a diff for Jev** (ADR 0053).
   - `queryFile` learns whether its content is a diff: `JudgeChange || mode == "diff"`, which also covers a truncated `--since` diff (review M1).
   - `parseDiff` keeps a hunk index; `Sides()` returns the before side (context and removed lines) and the after side (context and added lines), hunks separated by a blank line. A side that is empty or only whitespace isn't sent.
   - The `ask` is called once per side, checking `ctx` between calls (ADR 0024). A candidate's score is the higher of the two; an ADR with no rules still scores +Inf (passed on).
   - Tests: a hunk that only removes lines, one that only adds lines (one call), a mixed hunk, a truncated diff.
4. **`init --config`.**
   - `Init` gets the shared config option; a relative path resolves from the starting directory, like the other commands (review M3).
   - Missing file: today's prompts, then the full default config with the default pipeline.
   - Existing file without a list pipeline: asks once, "Replace analysis.pipeline with the default pipeline? (y/n)". On yes it splices the default list into the file text in place of the old block: from the `pipeline:` line to the last line indented deeper than it, so the next key's head comment stays. No `pipeline:` key: appended at the end of the `analysis:` block; no `analysis:` key: appended at the end of the file. It matches the file's indent width and line endings, and refuses flow style or YAML aliases in `analysis` with a message (review M2). If the old block had a `pre_judge` stage, it prints that the screen was removed and points to the README's `ask` node.
   - EOF or "n" cancels with exit 0, as today. An existing list pipeline: says so, changes nothing, exits 0.
   - Tests: a config with a `pre_judge` mapping keeps every other byte; a comment just above the next key; the pipeline as the last block; no `analysis:` key; CRLF.
   - Plain `init` without `--config`: see Q2.
5. **Wording and docs.**
   - User-facing "stage" text becomes "node": "node %s skipped", the rejected-key message, `stage/doc.go`, and the coverage line's "pre_judge screened out every candidate", which becomes "the <name> node dropped every candidate" (review H3).
   - README: the pipeline section for the list shape; "Screening with Jev" as an `ask` node; Jev sees both sides as plain text under `--since`, two Jev calls per file there, `--dry-run` included; `index` no longer needs the Jev key; a breaking-change migration note for CLI and Action users.
   - `.claude/rules/config.md`: drop the `scorerNames` bullet; the Jev key bullet says `check` reads it. `internal/config/doc.go` updated.
   - ADR statuses: 0049 to 0054 flip to `Accepted`; 0022 flips to `Superseded`.

## Verification

`go test -race ./...`, `go test -tags e2e ./test/e2e`, lint as CLAUDE.md says, `/code-review xhigh` on the branch before the PR.

## Review

Reviewed on `plan/452-node-list` by a read-only agent. Fixed from it: commits that build on their own (H1); pipeline parsed before the store or index loads (H2); coverage text naming `pre_judge` (H3); diff detection and hunk boundaries (M1); the `init` splice's end line, comments, CRLF and flow style (M2); `init --config` plumbing (M3); existing Jev tests (M4); the engine's built-in fallback (M5). Twelve implementation choices it listed are decided above; two that change what users see are questions.

## Open questions

- Q1: rename the JSON report's `stages` key to `nodes`? (card)
- Q2: plain `archguard init` on an existing `archguard.yaml`: splice the pipeline like `--config`, or keep today's overwrite prompt? (card)
