---
title: Candidate scoring pipeline
status: Accepted
scope: "internal/**"
---

# Candidate scoring pipeline

## Context

Which ADRs the LLM judges for a file should not depend on one scoring method. A scorer is anything that returns a 0-1 score per candidate ADR for a piece of code: embedding similarity, or a cheaper specialized model that screens for likely violations. Choosing candidates must be extensible without touching how files are gathered, baselined, or reported.

## Decision

Candidates flow through an ordered list of `stage.Stage`s (`internal/analysis/stage`) before LLM judgment. Every collaborator is an object with methods, not a bare function.

- `stage.Scorer` receives a `File`, a `Debug`, and all of a file's `Candidate`s in one call, and returns one score per candidate, in order, on the scale its stage's `Threshold` uses (0-1 for screening models; raw cosine similarity, -1 to 1, for `CosineRanker`). It only scores; it never drops or reorders.
- `stage.Stage` wraps a `Scorer` with a `Threshold` (the minimum score, optionally per ADR) and `MaxKeep`. The stage drops candidates below the threshold, orders survivors by descending score, and cuts to `MaxKeep`. The next stage receives only the survivors.
- `stage.File` exposes the path and the text to embed (`QueryText`, built lazily, diff-preferred and capped). `stage.Debug` is a real writer under `--debug` and a no-op otherwise, so scorers never nil-check.
- `stage.Error` carries the action that failed (`generating embedding`, `scoring candidates`); the engine reports `Error <action> for <file>` without knowing which scorer ran.
- `Engine.Stages` holds the pipeline. When unset it is one `stage.NewCosineStage`: `CosineRanker` scoring, an `ADRThreshold` (an ADR's own `similarity_threshold` over `vector_store.similarity_threshold`), and `analysis.max_relevant_adrs` as `MaxKeep`.
- The LLM is the only source of reported violations. Stages choose which ADRs are judged, never whether a file violates one.

The pipeline's input is built in a fixed order by `candidateSource`:

1. `VectorStore.ScopedADRs(file)` returns every ADR whose `scope` matches the file, without needing an embedding. Only scope-matched ADRs are ever candidates, whichever scorer runs first.
2. `archguard-ignore: <ADR_ID>` in the file's first 2000 bytes removes an ADR from that list, so a suppressed ADR never reaches a scorer, never occupies a top-K slot, and is never judged.
3. Each stage runs in order over the survivors.

Embedding is done by `CosineRanker` alone, through `llm.Embedder`, so a pipeline without it makes no embedding calls. `CosineRanker` asks the store for every qualifying ADR (topK unbounded) and scores a candidate the store omitted for missing its own threshold below any stage minimum; the stage applies the top-K cut. Under `--debug` it uses `SearchWithDebugInfo` so rejected ADRs keep their real scores (see `docs/arch/0019-single-query-consistency-for-debug-diagnostics.md`).

## Consequences

- Adding a scorer means implementing `stage.Scorer`; stage ordering, thresholds, caps, and reporting are unchanged.
- An embedding failure is reported as `Error generating embedding for <file>`; any other scorer failure as `Error scoring candidates for <file>`. Both count the file as skipped.
- Every `VectorStore` implementation provides `ScopedADRs`, and it returns an error rather than an empty list when the backend fails; the engine reports `Error loading candidate ADRs for <file>` and counts the file as skipped, so an unreachable database can't make a check pass.
- Equal scores keep candidate order (the store's order), so a tie at the top-K boundary is resolved deterministically.
- `--debug` prints `Skipping ADR ... (Suppressed)` for every suppressed scope-matched ADR first, then the stage's `Below threshold` (capped at `MaxKeep`) and `Cut by top-K limit` lines, whichever scorer produced the scores.
