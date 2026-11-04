---
paths:
  - "internal/inference/**"
---

# `internal/inference`

- **A new chat provider embeds the `temperature` holder and sends the vendor's field only when it is set.** Keep no list of models that reject the parameter; the user leaves `llm.temperature` unset for those.
- **`OllamaProvider` adds a task prefix only when the model name after its last `/` starts with an entry in `embeddingPrefixConventions`.** Any other model silently gets no prefix, with no warning.
- **Changing what text or settings go into an embedding call also changes `embeddingFormat` in `internal/index/embed.go`.** Every saved embedding is then rebuilt on the next index build (`docs/arch/0038-embedding-format-saved-with-the-index.md`). Without it, a rebuild reuses the saved embedding of every unchanged ADR and nothing detects the stale index.
- **A new `Asker` makes its refused-key and too-large errors match `ErrKeyRejected` and `ErrInputTooLarge` with `errors.Is`** (`docs/arch/0055-asking-is-an-inference-role.md`). Without that, a revoked key is treated as an outage and `on_error: skip` sends every file to the LLM.
