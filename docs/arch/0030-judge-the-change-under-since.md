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

- **`--since` makes the model read the file's diff.** `cli` sets `Engine.JudgeChange` from `--since`; no other scan sets it, and `--all`, paths, `--staged` and the default scan judge what they always have. `fetchContext` returns the hunks of `git diff <ref> HEAD -- <path>`, without the `diff --git`, `---` and `+++` header, and counts tokens on that. A file with no hunks (binary, mode-only) has nothing to judge and is skipped without being recorded as a gap. A diff over `llm.max_tokens` is truncated and `--ci` skips it, as with any oversize file.
- **The diff instructions are in the user prompt.** `inference.ChatPromptDiff` says the context is a unified diff and asks the model to judge the code the change leaves behind: an added line that contradicts the ADR, or a removed line the ADR requires. Removing code that itself broke the ADR is not a violation. `llm.system_prompt` replaces the system prompt wholesale, so the instructions are not there. `PromptTemplate` is part of the analysis cache key.
- **The quote is checked against the diff.** The diff is parsed into added, removed and unchanged lines. A finding counts when its `quoted_code` is in an added or removed line, and is dropped when it is only in unchanged context. A finding whose quote is nowhere in the diff stays `UNVERIFIED`. The finding's line is the new-file line of an added line, or the new-file line a removed line sat in front of.
- **`archguard-ignore: <ADR ID>` is read from the whole file,** because the header is usually outside the diff.
- **The engine cannot tell a required removal from a fix.** Both are quotes in removed lines, so the judgment is the model's, and the engine trusts it.

## Rules

- A `--since` check MUST send the model a file's diff, not the whole file.
- A finding whose quoted code is only in unchanged context MUST NOT be reported under `--since`.

## Alternatives considered

- **Only added lines count.** Deterministic, but deleting a required guard passes. A gate that is passed by deleting what the ADR requires is worse than one with occasional false positives.
- **Tell the model about the diff and trust it.** Old code within the 100 context lines of an edit can still fail the pull request, because nothing checks the quote.
- **Judge whole files and filter afterwards.** The model keeps full context, but nothing gets cheaper and a removal has no quote left to match.
- **A config key on every scan.** One more setting to document, and `--since` users would have to set it.

## Consequences

- A violation caused by a change elsewhere (a new parameter that makes an untouched call site wrong) is not reported under `--since`; a full `--all` run is the backstop.
- A weak model may report a removed violating line as a violation; the prompt tells it not to, and the engine cannot check.
- A quote that cannot be located in the diff stays unverified and still fails the run, so a model that paraphrases a real finding is reported, and so is one that paraphrases a finding in unchanged code. Whitespace differences inside a line are ignored; a multi-line quote that spans a blank line does not verify.
- Changing `ChatPromptDiff` changes the cache key, so earlier results are not reused.
