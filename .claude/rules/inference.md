---
paths:
  - "internal/inference/**"
---

# `internal/inference`

- **A new chat provider embeds the `temperature` holder and sends the vendor's field only when it is set.** Keep no list of models that reject the parameter; the user leaves `llm.temperature` unset for those.
- **`OllamaProvider` adds a task prefix only when the model name after its last `/` starts with an entry in `embeddingPrefixConventions`.** Any other model silently gets no prefix, with no warning.
- **Changing what text or settings go into an embedding call makes existing indexes stale, and nothing detects it.** Neither store's hash covers how embeddings are computed (`docs/arch/0003-embedding-task-role.md`), and a rebuild reuses the saved embedding of every unchanged ADR. Such a change needs a release note telling users to delete `.archguard/index.json` (or the project's Postgres rows) before running `archguard index` again.
