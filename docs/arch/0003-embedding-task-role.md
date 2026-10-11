---
title: Embedding task-role signal (query vs. document)
status: Accepted
scope: "internal/**"
---

# Embedding task-role signal (query vs. document)

## Context

Some embedding backends tune an embedding differently depending on whether the text is a document being indexed or a query searching that index. Gemini exposes this as `TaskType` (`RETRIEVAL_DOCUMENT` and `RETRIEVAL_QUERY`) and recommends setting it; nomic-embed-text uses `search_document:` and `search_query:` text prefixes. ArchGuard set neither, because `CreateEmbedding` had no way to say which role a call played. Each of ArchGuard's embedding call sites plays exactly one role: indexing embeds ADRs as documents, and the cosine stage embeds changed code as a query.

## Decision

`inference.Embedder.CreateEmbedding` takes an `EmbeddingTaskType`: document or query. Callers pass the role that matches what they are doing, not one that depends on the configured provider.

Each provider maps the role onto its backend's own mechanism. Gemini maps it to `TaskType`. Ollama maps it to a text prefix for embedding models with a documented prefix convention, held in a table, so a second model's convention is a new entry rather than a new code branch. OpenAI has no equivalent and ignores it.

## Consequences

- Every `inference.Embedder` implementation must accept `EmbeddingTaskType`, even one that ignores it.
- Asymmetric retrieval for a new provider or embedding model needs only a new mapping from the role to that backend's mechanism, never another interface change.
- Changing what an existing provider and model send for a role changes embeddings that an existing index has no record of. Index staleness checks key on ADR content and model name only, so an index built before such a change is compared against new query embeddings until it is rebuilt from scratch. The interface cannot prevent this; each such change must be called out in release notes and as a footgun in `.claude/rules/inference.md`.
