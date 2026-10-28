---
title: Jev ranks in one call per file and fails by default as the only ranker
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/config/**"
---

# Jev ranks in one call per file and fails by default as the only ranker

## Context

The pipeline (`docs/arch/0022-candidate-scoring-pipeline.md`) lets `rank` and `rerank` take any ranking scorer, and #196 asks for cosine and Jev to fill either, so one can narrow the candidates for the other. On the #202 spike, Jev ordered the ADRs that apply better than cosine (mean reciprocal rank 0.68 against 0.49) with about the same top-3 recall, but kept about 7 of 20 ADRs at 0.5, so it works best after a wide cosine cut. The spike asked about one ADR per call. Jev's documentation advises putting material the questions compare in one state object. Screening is `docs/arch/0047-jev-screens-in-pre-judge-and-fails-open.md`.

## Decision

- **`rank` and `rerank` accept `cosine` or `jev`.** `pre_judge` still accepts only a screening scorer.
- **A Jev ranking stage makes one call per file.** The state holds the file's judged content and each candidate's full ADR, and each candidate gets one question: is the code governed by this decision? Its score is that probability.
- **A Jev stage needs an explicit `threshold`,** since its probabilities share no scale with cosine similarity.
- **Under `on_error: skip`, a failed Jev ranking stage passes its candidates on unranked.** With `on_error` unset, a Jev stage that is the only ranker (no cosine `rank` or `rerank`) fails instead, since skipping it would send every scope-matched ADR to the LLM; any other Jev ranking stage skips.

## Rules

- A Jev ranking stage MUST ask about all of a file's candidates in one Jev call; it MUST NOT call Jev once per candidate.
- A Jev ranking stage whose `on_error` is unset MUST fail when Jev fails if no cosine stage runs in `rank` or `rerank`.

## Alternatives considered

- **Sending only the ADR sections the embedding uses.** A smaller state, but `docs/arch/0039-embed-selected-sections-judge-full-adr.md` keeps the section selection to the embedding.
- **One call per (file, ADR), as the spike measured.** Proven on the spike, but about 20 calls per file and against #196's rule that a stage scores a file's candidates together.
- **Jev as `rank` only, replacing cosine.** At 0.5 it kept about a third of the ADRs, so it narrows poorly on its own.
- **Default `on_error: skip` for every Jev stage.** A Jev outage would send every scope-matched ADR of every file to the LLM unannounced in cost.

## Consequences

- One call per file keeps Jev's cost near screening's, but the state grows with the number of candidates, so a large file with many scope-matched ADRs reaches Jev's size limit sooner and fails the stage as a precondition.
- The one-call form was not measured on the spike; its scores may differ from the per-pair results.
- A pipeline whose only ranker is Jev makes no embedding calls when checking files.
