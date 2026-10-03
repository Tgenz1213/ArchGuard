---
title: Decoupled chat and embedding providers
status: Accepted
scope: "internal/**"
---

# Decoupled chat and embedding providers

## Context

Every `inference.Provider` implementation (OpenAI, Ollama, Gemini) has always implemented `Chat`, `CreateEmbedding`, and `CountTokens` together, and `internal/cli.Execute` constructed exactly one provider instance from `llm.provider`, used for all three. Adding Claude support breaks this assumption: Anthropic's API has no embeddings endpoint at all, so a `ClaudeProvider` can chat and count tokens but cannot embed.

`config.VectorStore.Provider` (`vector_store.provider` in YAML) already existed in the config schema and appeared in every example config, but nothing in the codebase ever read it -- vector store *backend* selection (`LocalStore` vs `PgStore`) goes by `vector_store.connection_string`, and the embedding *model* backend was implicitly whatever `llm.provider` was.

## Decision

`vector_store.provider` becomes the explicit selector for which provider handles `CreateEmbedding`, decoupled from `llm.provider`, which selects the provider for `Chat`/`CountTokens`. When unset, it defaults to `llm.provider`, so a config that leaves it unset or sets it to the same value uses one provider for both roles.

Chat and embedding are separate roles in `internal/inference`: `inference.Chatter` (`Chat`, `CountTokens`) and `inference.Embedder` (`CreateEmbedding`). A provider implements only the roles its vendor supports, so `ClaudeProvider` is a `Chatter` and `VoyageProvider` an `Embedder`, with no method for the missing role. `inference.Provider` is both, for vendors that offer both through one client (OpenAI, Ollama, Gemini). Code takes the role it uses: `internal/analysis.Engine` has a `Chat` and an `Embed`, `BuildIndex` takes an `Embedder`, and the chat helpers take a `Chatter`, so a provider can't be wired into a role it lacks. `internal/cli` builds one instance for both roles when the two provider names match, and a chat provider and an embedding provider separately otherwise.

`internal/cli.validateProviderConfig` enforces three provider-pairing invariants that can't be expressed in the YAML schema itself:

- `llm.provider: "claude"` requires `vector_store.provider` to be set explicitly. There's no safe default to fall back to, since falling back to `llm.provider` itself would mean falling back to Claude, which can't embed.
- `vector_store.provider: "claude"` is rejected unconditionally. Claude can't embed regardless of which provider is handling chat, so it can never validly fill the embedding role either.
- `llm.provider: "voyage"` is rejected unconditionally, regardless of `vector_store.provider`. Voyage has no chat API at all -- unlike Claude, which at least has a valid *conditional* path as `llm.provider` once `vector_store.provider` is set, no config makes Voyage work as the chat provider.

All three fail fast at config load (`ExitConfig`, exit code 3) with a message naming the fix. The types already keep a provider out of a role it lacks; these checks are what give a wrong-role config a readable error.

`ARCHGUARD_EMBEDDING_API_KEY` is a new, optional environment variable, read only when the resolved embedding provider (`vector_store.provider`, or `llm.provider` when `vector_store.provider` is unset) differs from the chat provider. It does not fall back to `ARCHGUARD_API_KEY` when unset: whenever this branch runs, the embedding provider is necessarily a different vendor than the chat provider, so falling back would mean sending one vendor's API key to a different vendor's API. A credential should never cross a vendor boundary, so the embedding provider is built with an empty key (triggering the same "no API key set" warning any other misconfigured provider gets) rather than reusing `ARCHGUARD_API_KEY`. `ARCHGUARD_API_KEY` continues to supply the chat provider's key unchanged.

Two new providers land alongside this: `ClaudeProvider` (a `Chatter`: `Chat`/`CountTokens` via the official Anthropic Go SDK) and `VoyageProvider` (an `Embedder`, via Voyage AI's HTTP API -- no official Go SDK exists, so this is hand-rolled `net/http`, unlike the other three providers). Voyage is Anthropic's own documented recommendation for pairing with Claude, but `vector_store.provider` isn't restricted to Voyage -- `openai`, `ollama`, or `gemini` work equally well as the embedding provider for a `claude` chat provider.

`VoyageProvider.CreateEmbedding` supports both Voyage's `embed()` endpoint (`POST /v1/embeddings`, one vector per call) and `contextualized_embed()` endpoint (`POST /v1/contextualizedembeddings`, chunk-aware embeddings for `voyage-context-*` models), selected automatically by the configured model name. `contextualized_embed()` is always called with exactly one chunk per call (`inputs: [[text]]`), matching `CreateEmbedding`'s one-text-in/one-vector-out contract -- this gets access to Voyage's higher-quality contextualized models without requiring the rest of the codebase (`VectorStore.Search`, `ADR.Embedding`, `LocalStore`'s JSON schema, `PgStore`'s `archguard_adrs` table) to support multiple vectors per document. True multi-chunk-per-document indexing, where a long ADR is split into several chunks each embedded with awareness of the others, is a genuinely different and larger feature -- useful to any provider, not specific to Voyage or Claude -- and is deliberately not implemented here.

## Consequences

- A user pairing `llm.provider: claude` with any embedding-capable `vector_store.provider` gets a config-time error, not a runtime one, if they forget to set it, or if they set `vector_store.provider` to `claude` itself. `llm.provider: voyage` is rejected at config load unconditionally, regardless of `vector_store.provider`.
- A cross-vendor pairing (e.g. `llm.provider: claude` with `vector_store.provider: voyage`) requires two separate API keys, `ARCHGUARD_API_KEY` and `ARCHGUARD_EMBEDDING_API_KEY` -- there is no shared-key convenience path, by design, since the two providers are different vendors.
- `internal/cli` passes the resolved embedding provider to `BuildIndex` and the engine's `Embed`, and the chat provider to the engine's `Chat`; neither path can receive the other role's provider.
- Every future provider that can't do both chat and embeddings fits this same pattern: implement only the role its vendor supports (`inference.Chatter` or `inference.Embedder`), add it to the matching role builder in `internal/cli`, and let `vector_store.provider` (or, symmetrically, `llm.provider`, as Claude and Voyage already demonstrate for each direction) resolve the pairing. `validateProviderConfig` is the place to add a fail-fast check if the new provider can never validly fill one of the two roles.
- `cmd/archguard-e2e/main.go` supplies two distinct mock providers, one per role (`internal/cli.Execute`'s `ProviderFactories{Chat, Embed}` struct, where `Chat` returns an `inference.Provider` and `Embed` an `inference.Embedder`), reusing the chat mock for embedding only when the two provider names match, else failing fast if no embed factory was given -- `test/e2e_dual_provider_test.go` exercises the dual-provider split through a real `archguard index` + `archguard check` subprocess run, not just at the unit level.
