---
title: Embedding task-role signal (query vs. document)
status: Accepted
scope: "internal/**"
---

# Embedding task-role signal (query vs. document)

## Context

`GeminiProvider.CreateEmbedding` called Gemini's embedding API with a `nil` config, leaving `EmbedContentConfig.TaskType` unset. The Gemini API exposes `TaskType` values `RETRIEVAL_DOCUMENT` and `RETRIEVAL_QUERY` specifically to make asymmetric retrieval work: an embedding produced for "this is a document being indexed" and one produced for "this is a query searching that index" are tuned differently even for the same underlying text, and Gemini's own docs recommend setting `TaskType` for exactly this reason.

`llm.Provider.CreateEmbedding` had no way to signal which role a given call was playing. ArchGuard has exactly two embedding call sites, and they are unambiguously one role or the other: `internal/index/store.go` and `internal/index/pgvector.go` (`BuildIndex`) embed ADR content to index it; `internal/analysis/engine.go` (`Run`) embeds diff/code content to search the index.

## Decision

`llm.Provider.CreateEmbedding` gains an `EmbeddingTaskType` parameter (`EmbeddingTaskDocument` or `EmbeddingTaskQuery`). Callers pass the role that matches what they're doing, not what provider is configured — the two index call sites always pass `EmbeddingTaskDocument`, and `engine.go` always passes `EmbeddingTaskQuery`.

Only `GeminiProvider` acts on it, mapping the two roles to Gemini's own `TaskType` strings. `OpenAIProvider` and `OllamaProvider` accept and ignore the parameter — neither API exposes an equivalent mechanism today.

## Consequences

- Every current and future `llm.Provider` implementation must accept `EmbeddingTaskType`, even if it has nothing to do with it yet, to satisfy the interface.
- This is the Gemini-specific counterpart to a separate, still-open question: whether `OllamaProvider` should get an equivalent asymmetric-retrieval signal via a text convention (e.g. nomic-embed-text's `search_query:`/`search_document:` prefixes) instead of an API parameter, since Ollama has no `TaskType`-like config field. That would reuse this same `EmbeddingTaskType` value at the call sites; it does not require another interface change.
