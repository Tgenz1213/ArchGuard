---
title: Provider-scoped token counting
status: Accepted
scope: "internal/**"
---

# Provider-scoped token counting

## Context

Token counts decide where a file is truncated before it reaches the model. ArchGuard counted every model's tokens with an OpenAI tiktoken encoder, falling back to `cl100k_base` for any model tiktoken did not know. That is every non-OpenAI model, including the default Ollama model. A Llama token and a `cl100k_base` token do not span the same text, so truncation was wrong for every default install.

Bundling an exact offline tokenizer for every model family Ollama can serve would mean shipping per-family vocabulary data that drifts out of date.

## Decision

`inference.Chatter` has `CountTokens`. Each provider counts tokens with the mechanism that reflects its own backend's real tokenizer:

- OpenAI counts locally with tiktoken for the configured model.
- Ollama asks the live server to evaluate the prompt and reads back the prompt token count of the model actually loaded. This is exact for any model Ollama serves, with no bundled vocabulary.
- Gemini calls its own token-counting API.

Truncation estimates a cut point and verifies it with `CountTokens` until the text fits, since only tiktoken supports encoding and decoding slices locally.

If `CountTokens` fails, the error reaches the caller. A heuristic count is never substituted.

## Consequences

- Every `inference.Chatter` implementation must implement `CountTokens`. Most LLM APIs expose a native token count, so this stays easy to satisfy.
- Truncation for Ollama and Gemini costs one or more extra requests to the live backend. The backend must be reachable for chat anyway, so this adds latency on large files but no new dependency.
