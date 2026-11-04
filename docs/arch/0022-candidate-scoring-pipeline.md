---
title: Candidate scoring pipeline
status: Superseded
scope: "internal/**"
---

# Candidate scoring pipeline

## Context

Which ADRs the LLM judges for a file should not depend on one scoring method. A scorer is anything that returns a 0-1 score per candidate ADR for a piece of code: embedding similarity, or a cheaper specialized model that screens for likely violations. Choosing candidates must be extensible without touching how files are gathered, baselined, or reported.

## Decision

Candidates pass through an ordered list of stages before LLM judgment. Every collaborator is an object with methods, not a bare function.

- A scorer receives all of a file's candidates in one call and returns one score per candidate, in order, on the scale its stage's threshold uses. It only scores; it never drops or reorders.
- A stage wraps a scorer with a threshold, optionally per ADR, and a cap. It drops candidates below the threshold, orders the rest by descending score, and cuts to the cap. The next stage receives only the survivors.
- A stage error names the action that failed and its kind: a dependency that did not respond, or a precondition that is not met, such as cosine scoring with no embedding provider. The engine reports the error without knowing which scorer ran.
- With no `analysis.pipeline` block, the pipeline is one cosine stage, using `vector_store.similarity_threshold` and `analysis.max_relevant_adrs`. `analysis.pipeline` configures optional `rank` and `rerank` stages instead, validated when the config loads. An absent `rank` is the default cosine stage. An unset `rank` setting falls back to those same settings; an unset `rerank` setting uses a fixed default and prints a warning. An ADR's own `similarity_threshold` wins over the stage threshold in every cosine stage.
- The LLM is the only source of reported violations. Stages choose which ADRs are judged, never whether a file violates one.

The pipeline's input is fixed:

1. Only ADRs whose `scope` matches the file are candidates, whichever scorer runs first, and finding them needs no embedding.
2. An `archguard-ignore: <ADR_ID>` comment removes an ADR before any stage runs, so a suppressed ADR never reaches a scorer, never takes a top-K slot, and is never judged.
3. Each stage runs in order over the survivors.

Only the cosine scorer embeds, so a pipeline without it makes no embedding calls. Under `--debug`, rejected ADRs keep their real scores (see `docs/arch/0019-single-query-consistency-for-debug-diagnostics.md`).

## Rules

- Pipeline collaborators such as scorers and stages MUST be objects with methods, not bare functions.
- A scorer MUST only score its candidates; it MUST NOT drop or reorder them.
- A stage MUST NOT decide whether a file violates an ADR.
- An ADR suppressed by an `archguard-ignore: <ADR_ID>` comment MUST be removed before any stage runs.

## Alternatives considered

None recorded.

## Consequences

- Adding a scorer means implementing the scorer interface. Stage ordering, thresholds, caps and reporting stay the same.
- Scorer names are validated in `internal/config`, which cannot import the stage package, so a new scorer is also added to that list.
- Every cosine stage scores the same candidates, so a cosine `rerank` after a cosine `rank` only tightens `threshold` or `top_k`, and embeds the file a second time.
- A stage failure is reported under the file and counts the file as skipped, unless the stage sets `on_error: fail`. A run whose skips leave it with no ADR check fails (`docs/arch/0032-a-run-that-checked-nothing-fails.md`).
- `on_error` is set per stage. Under `fail`, the failure is recorded, that file's remaining stages stop, and other files still run. `check` then exits with a code for the failure's kind, which takes precedence over the drift exit code, because an incomplete check is not a clean verdict. `--update-baseline` exits the same way without writing the baseline. JSON output lists the failures, and omits the list when there are none.
- Every vector store finds scope-matched candidates and returns an error, not an empty list, when its backend fails. The file is reported and counted as skipped, so an unreachable database cannot make a check pass.
- Equal scores keep the store's order, so a tie at the top-K boundary is resolved deterministically.
- A stage with no candidates does not call its scorer, so a file with no candidates costs no embedding call.
- `--debug` shows the suppressed ADRs, then for each stage what it received and kept, and why each dropped ADR was dropped (below threshold or cut by top-K), whichever scorer produced the scores.
- Each stage has a name. Per stage over the run, the candidates received, the candidates kept and the time spent are totalled, safely across concurrent files, and `--format json` reports them. Every stage is listed, with zeros when it received nothing. The time is summed per file, so with concurrent files it can exceed wall-clock time.
