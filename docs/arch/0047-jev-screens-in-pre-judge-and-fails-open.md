---
title: Jev screens candidates in pre_judge and fails open
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/config/**"
---

# Jev screens candidates in pre_judge and fails open

## Context

Every ADR that survives ranking costs a full LLM compliance call, and most conclude "no violation". TypeSafe's Jev answers many yes/no questions about one text in a single cheap call, with a probability and no reasoning. The #202 spike found it near-perfect on literal, single-condition rules (violations 0.94-0.99, compliant code 0.03-0.16), weak on LLM-generated questions, misled by marker-stripped diffs, and less proven as a relevance ranker than as a violation screen. The pipeline (`docs/arch/0022-candidate-scoring-pipeline.md`) has a slot for a stage between ranking and judgment.

## Decision

- **Jev runs only as the `pre_judge` stage, after ranking.** It screens for likely violations; it does not rank. `pre_judge` accepts only a scorer that screens for violations, and `rank` and `rerank` reject one.
- **One Jev call per file asks one question per rule of every candidate ADR that has rules.** An ADR's score is its highest rule probability, and the stage keeps ADRs scoring at or above its required `threshold`.
- **An ADR without rules passes through the stage unchanged** and takes no `top_k` slot, since Jev has nothing to ask about it.
- **Jev sees exactly what the LLM would judge,** a diff keeping its `+`/`-` markers included.
- **The stage fails open.** Under `on_error: skip`, the default, a Jev failure passes every candidate on to the LLM with a warning, instead of skipping the file. Under `on_error: fail` it fails the check, as any stage does. A file over Jev's size limit is a precondition failure; any other failure means Jev is unavailable. Rate-limited and overloaded responses are retried first.
- **The API key comes only from `ARCHGUARD_JEV_API_KEY`,** and its absence is a config error whenever a Jev stage is configured.

## Rules

- A screening stage MUST NOT report a violation; only LLM judgment reports one.
- A screening stage MUST pass an ADR that has no rules through unchanged.
- A screening stage MUST send the screener the same content the LLM judges, never a diff stripped of its markers.
- A screening stage whose `on_error` is `skip` or unset MUST pass its candidates on unchanged when it fails; it MUST NOT drop them or skip the file. Failing the check under `on_error: fail` is allowed.
- Scorer credentials MUST be read from environment variables, never from `archguard.yaml`.

## Alternatives considered

- **Jev as `rank` or `rerank`.** Its relevance ordering beat cosine's on the spike but its top-3 recall did not, and it kept about 7 of 20 ADRs at 0.5. Left for a later decision.
- **Fail closed (drop candidates when Jev fails).** A Jev outage would silently stop violations from being found.
- **Skip the file when Jev fails, as a failed cosine stage does.** Ranking cannot run without its scorer, but judgment can run without screening, only at a higher cost.
- **One Jev call per ADR or per rule.** More calls and latency for the same answers.
- **Questions generated from the whole ADR by default.** They caught 2-3 of 5 seeded violations on the spike; authored rules are the default, and generation is opt-in.

## Consequences

- A check with a Jev stage makes fewer LLM calls on ADRs with rules, and the same number on ADRs without them.
- A Jev outage under the default makes a check cost what it would without the stage.
- File content leaves the machine for TypeSafe's service whenever a Jev stage is configured.
- Each judged ADR's logged score is from the last stage that scored it, so a Jev probability replaces a cosine similarity.
- Jev's tokens are not in the run's usage report.
