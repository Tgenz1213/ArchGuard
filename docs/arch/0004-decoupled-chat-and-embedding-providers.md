---
title: Decoupled chat and embedding providers
status: Accepted
scope: "internal/**"
---

# Decoupled chat and embedding providers

## Context

Every provider (OpenAI, Ollama, Gemini) implemented chat, embedding and token counting together, and one provider, chosen by `llm.provider`, filled all three roles. Claude breaks that assumption: Anthropic's API has no embeddings endpoint, so a Claude provider can chat and count tokens but cannot embed.

`vector_store.provider` already existed in the config schema, but nothing read it. The vector store backend is chosen by `vector_store.connection_string`, and the embedding backend was implicitly whatever `llm.provider` was.

## Decision

`vector_store.provider` selects the embedding provider, separately from `llm.provider`, which selects the chat provider. When it is unset it defaults to `llm.provider`, so one provider serves both roles.

Chat and embedding are separate roles in `internal/inference`: `inference.Chatter` and `inference.Embedder`. A provider implements only the roles its vendor supports. Claude is a `Chatter` and Voyage an `Embedder`, with no method for the missing role. `inference.Provider` is both, for vendors that offer both through one client. Code takes the role it uses, so a provider cannot be wired into a role it lacks.

Config validation enforces three pairing invariants the YAML schema cannot express. Each fails fast at config load with a message naming the fix:

- `llm.provider: "claude"` requires `vector_store.provider` to be set explicitly. Falling back to `llm.provider` would mean falling back to Claude, which cannot embed.
- `vector_store.provider: "claude"` is always rejected. Claude can never fill the embedding role.
- `llm.provider: "voyage"` is always rejected. Voyage has no chat API, so no config makes it work as the chat provider.

The types already keep a provider out of a role it lacks. These checks give a wrong-role config a readable error.

When the embedding provider differs from the chat provider, its key comes from `ARCHGUARD_EMBEDDING_API_KEY`. It does not fall back to `ARCHGUARD_API_KEY`. The two providers are then different vendors, and a credential must never cross a vendor boundary. An unset embedding key gets the usual "no API key set" warning instead.

Claude and Voyage land with this decision. Voyage is Anthropic's recommended pairing for Claude, but any embedding-capable provider can fill the embedding role. Voyage has no official Go SDK, so its client is hand-rolled over `net/http`.

Voyage's contextualized models are supported by sending exactly one chunk per call. That keeps the one-text-in, one-vector-out embedding contract, so the vector stores need not hold several vectors per document.

## Rules

- A provider MUST implement only the roles its vendor supports.
- Code MUST take the role it uses (`inference.Chatter` or `inference.Embedder`), so a provider cannot be wired into a role it lacks.
- Config validation MUST fail fast at config load, with a message naming the fix, when `llm.provider` is `"claude"` and `vector_store.provider` is not set explicitly, when `vector_store.provider` is `"claude"`, or when `llm.provider` is `"voyage"`.
- When the embedding provider differs from the chat provider, its key MUST come from `ARCHGUARD_EMBEDDING_API_KEY` and MUST NOT fall back to `ARCHGUARD_API_KEY`.
- Voyage's contextualized models MUST be sent exactly one chunk per call.

## Alternatives considered

- **Multi-chunk indexing, where a long ADR is split into chunks embedded with awareness of each other.** It is a larger feature useful to every provider, and is deliberately not part of this decision.

## Consequences

- A Claude chat config that omits `vector_store.provider`, or sets it to `claude`, fails at config load, not at run time. So does any config with `llm.provider: voyage`.
- A cross-vendor pairing needs two API keys. There is no shared-key shortcut, by design.
- The chat provider and the embedding provider travel separately through the CLI, the engine and the index build, and neither path can receive the other role's provider.
- A future provider that supports only one role follows the same pattern: implement that role, register it with that role's builder, and let the two config keys resolve the pairing. Config validation is the place for a fail-fast check if it can never fill one of the roles.
- The e2e harness supplies one mock provider per role and exercises the split through real `archguard index` and `archguard check` runs.
