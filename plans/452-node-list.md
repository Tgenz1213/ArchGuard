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

## Changes, one commit each

1. **Config: keep the pipeline raw at load, parse it at `check`** (ADR 0051).
   - `config.Analysis.Pipeline` becomes the raw `yaml.Node`; `LoadConfig` never inspects it, so `archguard index` loads any pipeline.
   - New `config.ParsePipeline(cfg) ([]Node, error)`: list-shaped or exit-3 error; a mapping (the old slots) gets the "run `archguard init`" message with the default pipeline to paste; then validation rules 1-8 limited to these three kinds, each error naming the node.
   - `Node` holds `Name`, `Job`, `With`, `Threshold`/`TopK` (rank), `Question` (ask: type, for_each, instructions) and `Keep` (threshold, top_k), `OnError`.
   - Presets: `breaks-a-rule` only, filling `type: noul`, `for_each: rule`, `instructions: "Does this code break the following rule? {rule}"`, `keep.threshold: 0.3`. Setting `type`, `for_each`, `instructions` or `criteria` beside a preset is an error.
   - Placeholders: `{rule}` required with `for_each: rule`; `{code}`, `{adr}` and any other name are errors. `for_each: adr`, `choice` and `score` are rejected as not yet supported (they arrive in #454 and #455).
   - Top-level `jev:` block with `model`. `JevOptions` and `StageConfig` go.
   - The Jev key leaves `LoadConfig`: `check` reads `ARCHGUARD_JEV_API_KEY` when the pipeline has a Jev node (ADR 0054). `index` no longer needs it.
   - Removed: `Pipeline`, `StageConfig`, `decodeStage`, `scorerNames`/`rankingScorers`/`screeningScorers`.
2. **Analysis: build stages from nodes.**
   - `BuildStages` is replaced by `BuildPipeline(cfg, nodes, ...)` returning the stages; `rankStage`/`rerankStage`/`preJudgeStage` go.
   - A stage's name is its node's name, so `--debug`, the JSON `stages` list and failure messages show node names.
   - `on_error` default per ADR 0052. A first node is always a `rank` here (validation), so the first-node `ask` rule has nothing to reject yet.
   - `JevScreen` builds each question from the node's `instructions`, replacing `{rule}` with the rule's statement and examples exactly as `questionText` does today, so the preset sends byte-identical questions.
   - Stable tie order at the top-K cut gets a test (it is `sort.SliceStable` with no test today).
3. **Analysis: both sides of a diff for Jev** (ADR 0053).
   - When the file is judged by its diff (`--since`, or ADR 0037's oversized-file diff), the Jev `ask` is called twice: the before side (context and removed lines) and the after side (context and added lines), each plain text, hunks separated by a blank line. A side with no lines isn't sent.
   - A candidate's score is the higher of the two; an ADR with no rules still scores +Inf (passed on). Tests: a hunk that only removes lines, one that only adds lines (one call), a mixed hunk.
   - `stage.File` gets a `Sides()` method; `JudgedText()` stays for the LLM.
4. **CLI: `check` builds the pipeline; `init --config`.**
   - `check` calls `ParsePipeline` before it judges any file and exits 3 on any error.
   - `selectScreener` reads the Jev key and model from the node list and `jev:`.
   - `init` gains `--config <path>`. Missing file: today's prompts, then the full default config with the default pipeline. Existing file without a list pipeline: asks once, "Replace analysis.pipeline with the default pipeline? (y/n)", then splices the default list into the file text by the YAML node's line range (or appends it under `analysis:`), so every other byte, comment included, is unchanged. EOF or "n" cancels with exit 0, as today's overwrite prompt does. An existing list pipeline: says so and changes nothing.
5. **Configs and docs.**
   - `archguard.yaml`: `shortlist` rank, `breaks-a-rule` ask (today's `pre_judge` at 0.3), `judge`.
   - `.github/archguard-ci.yaml`: the default pipeline, so this repo's drift check keeps running.
   - README: the pipeline section rewritten for the list shape; "Screening with Jev" becomes the `ask` node; `index` no longer needs the Jev key; the JSON `stages` entries carry node names; migration note for existing configs.
   - `.claude/rules/config.md`: drop the `scorerNames` bullet; the Jev key bullet says `check` reads it. `internal/config/doc.go` updated.
   - ADR statuses: 0049 to 0054 flip to `Accepted`; 0022 flips to `Superseded`.
6. **Tests.**
   - Unit: every validation rule fails with exit 3 and the node's name; fallbacks for `rank`; preset byte-identical questions; sides.
   - `index` succeeds on a config `check` rejects (e2e).
   - `init --config` on a config with a `pre_judge` mapping keeps every other line byte-for-byte (test diffs the file).
   - e2e: every existing e2e config gains the default pipeline; their expected findings and exit codes stay the same, which is the "same as today's no-pipeline run" check. The Jev e2e tests move to the list shape.

## Verification

`go test -race ./...`, `go test -tags e2e ./test/e2e`, lint as CLAUDE.md says, `/code-review xhigh` on the branch before the PR.

## Open questions

- Q1: rename the JSON report's `stages` key to `nodes`? (card)
