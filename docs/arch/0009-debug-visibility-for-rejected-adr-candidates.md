---
title: Debug visibility for rejected ADR candidates
status: Accepted
scope: "internal/**"
---

# Debug visibility for rejected ADR candidates

## Context

A vector store drops ADRs that score below the similarity threshold before the engine sees them, and `--debug` logged only the ADRs that survived. Even in `--debug` there was no way to tell apart an ADR that does not exist, one that scored too low, and one that was never embedded: all three looked like silence. See #137.

## Decision

`VectorStore` has a separate method, `SearchRejected`, that returns the scope-matched candidates scoring below the threshold. It has the same scope-filter-then-rank shape as `Search` (`docs/arch/0007-scope-filtered-before-topk-similarity.md`) and keeps the complement of the threshold filter at the same point in the pipeline. Under `--debug` the cosine stage logs each rejected candidate's title and score. The method runs only under `--debug`, so a normal run does no extra work and prints nothing new.

Making `Search` always compute rejected candidates, and letting the engine decide whether to print them, was rejected. Every call on both backends would do more work, including the non-debug path that nearly all real runs take. A separate, debug-only method keeps that path untouched.

## Consequences

- `--debug` shows, per file, which scope-matched ADRs were excluded for scoring below the threshold, with their title and score.
- On Postgres, `SearchRejected` issues a second query that costs about as much as `Search`. This happens only under `--debug`, so `check --ci` cost is unchanged.
- Both backends must keep `SearchRejected` aligned with `Search`'s scope and threshold filtering. The keep and reject threshold filters both key off a single threshold predicate, so a change to threshold semantics happens in one place and the two filters stay exact complements.
