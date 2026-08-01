---
title: Embedding task-role signal (query vs. document)
status: Accepted
scope: "internal/**"
---

# Embedding task-role signal (query vs. document)

## Context

`GeminiProvider.CreateEmbedding` called Gemini's embedding API with a `nil` config, leaving `EmbedContentConfig.TaskType` unset. The Gemini API exposes `TaskType` values `RETRIEVAL_DOCUMENT` and `RETRIEVAL_QUERY` specifically to make asymmetric retrieval work: an embedding produced for "this is a document being indexed" and one produced for "this is a query searching that index" are tuned differently even for the same underlying text, and Gemini's own docs recommend setting `TaskType` for exactly this reason.

`llm.Provider.CreateEmbedding` had no way to signal which role a given call was playing. ArchGuard has three embedding call sites, and each is unambiguously one role or the other: `internal/index/store.go` and `internal/index/pgvector.go` (`BuildIndex`) embed ADR content to index it; `internal/analysis/engine.go` (`Run`) embeds diff/code content to search the index.

## Decision

`llm.Provider.CreateEmbedding` gains an `EmbeddingTaskType` parameter (`EmbeddingTaskDocument` or `EmbeddingTaskQuery`). Callers pass the role that matches what they're doing, not what provider is configured — the two index call sites always pass `EmbeddingTaskDocument`, and `engine.go` always passes `EmbeddingTaskQuery`.

`GeminiProvider` maps the two roles to Gemini's own `TaskType` strings. `OllamaProvider` maps them to nomic-embed-text's documented `search_document:`/`search_query:` text-prefix convention, gated on the configured embed model name starting with `nomic-embed` — no other Ollama embedding model is documented to use this convention, so the prefix is skipped for anything else. `OpenAIProvider` accepts and ignores the parameter — the OpenAI embeddings API has no equivalent mechanism.

## Consequences

- Every current and future `llm.Provider` implementation must accept `EmbeddingTaskType`, even if it has nothing to do with it yet, to satisfy the interface.
- Adding asymmetric retrieval for a new provider or embedding model never requires another interface change, only a new mapping from `EmbeddingTaskType` to that backend's own mechanism (API parameter, text prefix, or none).
