---
paths:
  - "internal/inference/**"
---

# `internal/inference`

Chat and embedding are separate roles: `Chatter` (`Chat`, `CountTokens`) and `Embedder` (`CreateEmbedding`). A provider implements only the roles its vendor offers (`ClaudeProvider` chats, `VoyageProvider` embeds), and `Provider` is both. Code takes the role it uses; `Provider` appears only where one instance fills both roles. See `docs/arch/0004-decoupled-chat-and-embedding-providers.md`.

- `CountTokens` uses each backend's own tokenizer, and for Ollama and Gemini it is a network call, so truncating an oversized file costs several round trips (`docs/arch/0002-provider-scoped-token-counting.md`).
- `CreateEmbedding` takes the call's role (document or query); each provider maps it onto its backend's convention, or ignores it (`docs/arch/0003-embedding-task-role.md`).
- `SuggestRemediation` and `AnalyzeDrift` share retries and JSON parsing through `chatJSON[T]` (`docs/arch/0016-llm-suggested-remediation.md`).
- Prompt text is part of the analysis and suggestion cache keys, so editing a prompt invalidates earlier results. The `--since` diff instructions are in the user prompt `ChatPromptDiff`, not the system prompt (`docs/arch/0030-judge-the-change-under-since.md`).

## Footguns

- **`OllamaProvider` adds the nomic task prefix only when the last `/`-separated segment of `vector_store.model` starts with `nomic-embed`.** Any other name silently gets no prefix, with no warning.
- **Changing what text or settings go into an embedding call makes existing indexes stale, and nothing detects it.** Neither store's hash covers how embeddings are computed (`docs/arch/0003-embedding-task-role.md`), so such a change needs a release note telling users to re-run `archguard index`.
