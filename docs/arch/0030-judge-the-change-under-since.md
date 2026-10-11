---
title: Under --since the model judges the change, not the whole file
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/inference/**"
  - "internal/cli/**"
---

# Under --since the model judges the change, not the whole file

## Context

`check --since <ref>` selects the files a change touched. Judging each of those files whole makes code the change never touched fail the pull request, and costs tokens for lines nobody changed. A change can also break an ADR by removing something it requires (a guard, an error check, a context parameter), which a judgment of the new file cannot see: the removed code is no longer there to quote.

## Decision

- **`--since` makes the model read the file's diff against the ref.** Only `--since` does this; every other scan judges whole files. A file whose diff has no hunks has nothing to judge and is not a gap. A diff over the token limit is treated like any oversize file.
- **`--whole-file` opts back into judging each changed file in full.** It is for a team that wants every file a pull request touches to be left compliant. `--since` still picks the files, so a violation on a line the change did not touch is reported and a removed line is not seen. `--whole-file` without `--since` is a usage error.
- **The model judges the code the change leaves behind.** An added line that contradicts the ADR is a violation, and so is a removed line the ADR requires. Removing code that itself broke the ADR is not. These instructions are in the user prompt, so a custom system prompt does not drop them, and the prompt is part of the analysis cache key.
- **The quote is checked against the diff.** A finding counts when its quote is in an added or removed line, and is dropped when it is only in unchanged context. A quote that is nowhere in the diff is unverified (`docs/arch/0031-unverified-findings-and-the-exit-code.md`).
- **`archguard-ignore` is read from the whole file,** because the header is usually outside the diff.
- **The engine cannot tell a required removal from a fix.** Both are quotes in removed lines, so the judgment is the model's, and the engine trusts it.

## Rules

- A `--since` check MUST send the model each file's diff hunks, not the whole file, unless `--whole-file` is set.
- A `--since` finding whose quoted code is only in unchanged context lines MUST NOT be reported, unless `--whole-file` is set.

## Alternatives considered

- **Only added lines count.** Deterministic, but deleting a required guard passes. A gate that is passed by deleting what the ADR requires is worse than one with occasional false positives.
- **Tell the model about the diff and trust it.** Old code in the context lines around an edit can still fail the pull request, because nothing checks the quote.
- **Judge whole files and filter afterwards.** The model keeps full context, but nothing gets cheaper and a removal has no quote left to match.
- **A config key on every scan.** One more setting to document, and `--since` users would have to set it.
- **Whole files by default, with a `--diff-only` opt-in.** Every CI user would have to remember the flag to get the cheaper, change-scoped check.
- **A `--judge change|file` option instead of `--whole-file`.** More extensible, but nothing but `--since` needs a second judging mode yet.

## Consequences

- A violation caused by a change elsewhere (a new parameter that makes an untouched call site wrong) is not reported under `--since`; a full `--all` run is the backstop.
- A weak model may report a removed violating line as a violation; the prompt tells it not to, and the engine cannot check.
- A model that paraphrases a quote gets an unverified finding, which is listed but does not fail the run, whether the finding was real or in unchanged code.
- Changing the diff prompt changes the cache key, so earlier results are not reused.
