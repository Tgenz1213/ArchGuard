---
title: "Shorten an over-long embedding input instead of skipping it"
status: "Accepted"
scope: ["internal/index/**", "internal/analysis/**", "internal/inference/**"]
---

# Shorten an over-long embedding input instead of skipping it

## Context

An embedding model accepts a limited input. When a changed file's query text or an ADR exceeded it, the provider returned an error and ArchGuard skipped that file or ADR, so it was never checked or never applied. With Ollama `nomic-embed-text`, 35 of 480 changed files across this repository's 60 most recent code commits, and ADR 0007, failed this way (#228).

The limit depends on the model and, for Ollama, on the server's context size, so ArchGuard cannot know it in advance. Providers also differ in what they do with an over-long input. Measured on 2026-10-11: Ollama's `/api/embeddings`, OpenAI `text-embedding-3-small` and Voyage `voyage-context-3` reject it; Gemini (`gemini-embedding-2` and `gemini-embedding-001`) and Voyage `voyage-4` truncate it on the server without saying so.

## Decision

Every embedding call goes through `inference.EmbedToFit`. A provider that rejects an input as too long returns an error wrapping `inference.ErrInputTooLong`, identified by the status and message its API sends. `EmbedToFit` then cuts the text to about three quarters, ending at a line break where it can, and tries again, up to 12 times. The caller notes under `--debug` when the text it embedded is shorter than what it asked for.

Rejected alternatives:

- **A table of input limits per model.** It goes stale with every new model, and an Ollama model's limit depends on the server's configuration.
- **Turning on server-side truncation everywhere.** Ollama offers it only on `/api/embed`, a different endpoint from the `/api/embeddings` ArchGuard calls, and OpenAI has none. It would also hide when text was cut.
- **Counting tokens before embedding.** `CountTokens` belongs to the chat model, not the embedding model, and is a network call for Ollama and Gemini.

## Rules

- Code that embeds text MUST call `inference.EmbedToFit`, not `CreateEmbedding` directly.
- A provider whose API rejects an over-long embedding input MUST wrap that error in `inference.ErrInputTooLong`.

## Consequences

- A file or ADR of any length is embedded, using as much of its start as the model accepts. The end of a long ADR does not influence which files it matches.
- Detection relies on each API's error message. If a provider rewords it, that provider's over-long inputs are skipped again, with the provider's error, as before this decision.
- Gemini and Voyage's non-contextualized models never report a cut, so `--debug` cannot note one for them.
- An over-long input costs up to 13 embedding calls instead of one, all but the last of them rejected.
