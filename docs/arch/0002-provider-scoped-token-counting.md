---
title: Provider-scoped token counting
status: Accepted
scope: "internal/**"
---

# Provider-scoped token counting

## Context

Token counts drive truncation decisions in `Engine.fetchContext`. The original implementation always built an OpenAI tiktoken encoder off `LLM.Model`, regardless of `LLM.Provider`, and silently fell back to `cl100k_base` whenever tiktoken didn't recognize the model name — which is every non-OpenAI model, including the `ollama` / `llama3.2` default. A Llama token and a cl100k_base token don't span the same text, so truncation boundaries computed this way were wrong for every default install.

There is no practical way to bundle an offline, exact tokenizer for every model family Ollama can serve (Llama, Mistral, Qwen, Gemma, Phi, ...) without shipping per-family vocab data that would drift out of date.

## Decision

`inference.Chatter` has `CountTokens(ctx, text) (int, error)`. Each provider counts tokens using whatever mechanism reflects its own backend's real tokenizer:

- `OpenAIProvider`: local tiktoken, keyed off the configured model name, falling back to `cl100k_base` for unrecognized OpenAI model names (unchanged from the original behavior).
- `OllamaProvider`: asks the live Ollama server to evaluate the prompt with `num_predict: 1` (generate exactly one token — `num_predict: 0` was tried first and does not suppress generation on this codebase's Ollama version) and reads back the real `prompt_eval_count` from whatever model is actually loaded. This is exact for any model Ollama serves, with no bundled vocab data.
- `GeminiProvider`: calls Gemini's own `CountTokens` API.

`Engine.fetchContext` truncates by estimating a byte cutoff from the content's average bytes-per-token ratio, then verifies and shrinks that estimate via `CountTokens` calls until it fits, then rolls back to the nearest newline. This replaces the old encode-slice-decode approach, which only tiktoken supports.

If `CountTokens` errors (e.g. the Ollama server is unreachable or the model isn't loaded), the error propagates to the caller instead of silently substituting a heuristic.

## Consequences

- Every `inference.Chatter` implementation must implement `CountTokens`. Most LLM APIs expose a native token-counting mechanism, so this is expected to stay easy to satisfy.
- Truncation for Ollama and Gemini costs one or more extra requests to the live backend, rather than being pure local computation. `Engine.Chat` has to be live for `Chat` anyway, so this adds no new reachability dependency, only latency on large files that need truncation.
