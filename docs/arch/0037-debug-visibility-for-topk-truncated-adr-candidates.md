---
title: Debug visibility for topK-truncated ADR candidates
status: Accepted
scope: "internal/**"
---

# Debug visibility for topK-truncated ADR candidates

## Context

`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md` makes `--debug` show ADRs that scored below the similarity threshold. An ADR that passes both scope and threshold can still be dropped by the topK limit, and in `--debug` that looked the same as no other ADR being relevant. See #190.

## Decision

This extends 0009 and supersedes none of it. `VectorStore` has `SearchTruncated`, which returns the candidates that pass scope and threshold but fall outside the topK limit. It follows 0009's shape and cost contract: it complements the topK cut instead of the threshold filter, and it runs only under `--debug`. Under `--debug` each truncated candidate is logged with its score and rank, on a line distinct from the below-threshold line.

The result is not capped. Every qualifying ADR beyond topK is reported, not only the closest few.

Under `--debug` the hits and both diagnostics come from one query (`docs/arch/0019-single-query-consistency-for-debug-diagnostics.md`).

## Consequences

- `--debug` tells an ADR cut by the topK limit apart from one that was never relevant.
- A normal run does no extra work and prints nothing new.
- An uncapped list can be long when a corpus has many qualifying ADRs for one file.
