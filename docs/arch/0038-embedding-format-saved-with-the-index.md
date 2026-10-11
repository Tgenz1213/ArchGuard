---
title: The embedding format is saved with the index
status: Accepted
scope:
  - "internal/index/**"
---

# The embedding format is saved with the index

## Context

A rebuild of either index reuses the saved embedding of every ADR whose text did not change (#279). How an ADR becomes an embedding can still change between releases: the text `internal/index` builds from it, or what a provider adds to it, such as the task prefix in `docs/arch/0003-embedding-task-role.md`. The ADR text is then the same, so every saved embedding is reused although it no longer matches the query embeddings, and the only remedy was deleting the index by hand.

## Decision

- **An embedding format is saved with every embedding.** The local index saves it once for the whole file, and Postgres saves it on each row.
- **The format is a constant in `internal/index`,** changed in the same change that alters how an ADR's embedding input is built, in `internal/index` or in any provider.
- **An embedding saved in another format is never reused.** The next index build embeds that ADR again, and `archguard check` rebuilds a local index whose format differs. An index from before the format was saved counts as another format.

## Rules

- A saved embedding MUST NOT be reused unless its saved embedding format equals the current one.
- Every embedding a store saves MUST be saved together with the current embedding format.

## Alternatives considered

- **An `archguard index --reembed` flag.** Every user would have to read the release notes and remember to run it, and nothing tells them an index is stale.
- **A hash of the exact text and settings sent to the embedding API.** Automatic, but the index would have to ask each provider what it adds to the text and which request settings it uses before every call, which widens `inference.Embedder` for every provider.

## Consequences

- The first index build after upgrading to this format embeds every ADR once.
- A change to the embedding input that does not change the constant still goes undetected; the footgun in `.claude/rules/inference.md` names the constant.
- A setting that changes the embedding input, such as choosing which ADR sections are embedded (#345), joins the format instead of adding its own check.
