---
paths:
  - "internal/config/**"
  - "archguard.yaml"
---

# `internal/config`

Defines `Config` (YAML-backed). `analysis.pipeline` (`Pipeline` in `pipeline.go`, `StageConfig` in `stage.go`) declares optional `rank` and `rerank` stages, each with `scorer` (`cosine`, the default), `threshold` (0-1), `top_k` (positive) and `on_error` (`skip`, the default, or `fail`); `Pipeline.UnmarshalYAML` rejects an unrecognized stage or stage key, and every bad value fails inside `LoadConfig` so it exits `ExitConfig` naming the stage and key. `Analysis.RelevantADRLimit()` is the one place `max_relevant_adrs` defaults to 3. `analysis.rules_heading` names the ADR body section read for rules; blank means `index.DefaultRulesHeading` (`Rules`). `ARCHGUARD_DB_URL` env var overrides `vector_store.connection_string`; `ARCHGUARD_API_KEY` supplies the chat provider's (`llm.provider`) API key, and `ARCHGUARD_EMBEDDING_API_KEY` supplies the embedding provider's (`vector_store.provider`) key when it differs from the chat provider — both read in `cli`, not `config` (see `docs/arch/0004-decoupled-chat-and-embedding-providers.md`).

## Footguns

- **`archguard.yaml` is not gitignored, but `analysis.confluence.token` lives in it in plaintext** (unlike the DB connection string and LLM API key, which both have environment-variable overrides — `ARCHGUARD_DB_URL`, `ARCHGUARD_API_KEY`). Don't commit a real Confluence token into `archguard.yaml`; if you're adding config secrets, prefer wiring them through an env var like the existing two rather than a YAML field.
- **`vector_store.reindex_enabled` and `vector_store.reindex_concurrently` are `*bool`, not `bool`, in `config.VectorStore` — both default to `true` when the YAML key is absent.** `vector_store.reindex_threshold` is `*float64` and defaults to `0.20` only when nil, so an explicit `0` (reindex on any churn at all) is honored. The reason is the zero-value rule in `docs/standards/paradigm.md`. `vector_store.iterative_scan` follows the same nil-default-true `*bool` pattern; whether it takes effect also depends on the installed pgvector (see the `internal/index` rules).
