---
paths:
  - "internal/config/**"
  - "archguard.yaml"
---

# `internal/config`

- Every bad `analysis.pipeline` value fails inside `LoadConfig` as a config error naming the stage and key. A new scorer name is added to the list here too, since `config` can't import `stage` (`docs/arch/0022-candidate-scoring-pipeline.md`).
- `Analysis.RelevantADRLimit()` is the one place `max_relevant_adrs` defaults to 3.
- The env vars `ARCHGUARD_DB_URL`, `ARCHGUARD_API_KEY` and `ARCHGUARD_EMBEDDING_API_KEY` are read in `internal/cli`, not here.
- A setting whose zero value is meaningful is a pointer that defaults when nil (`reindex_enabled`, `reindex_concurrently`, `reindex_threshold`, `iterative_scan`), per the zero-value rule in `docs/standards/paradigm.md`.

## Footguns

- **`analysis.confluence.token` is plaintext in `archguard.yaml`, which is not gitignored.** The DB URL and API keys have env-var overrides; a new secret should get one too rather than a YAML-only field.
