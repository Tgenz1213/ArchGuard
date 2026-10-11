---
paths:
  - "internal/config/**"
  - "archguard.yaml"
---

# `internal/config`

- **A new scorer is added to `scorerNames` here as well as to package `stage`,** because `config` cannot import `stage` and rejects an unknown name when it loads (`docs/arch/0022-candidate-scoring-pipeline.md`).
- **Read `max_relevant_adrs` through `Analysis.RelevantADRLimit()`,** never the field, so its default applies in one place.
- **`analysis.confluence.token` is plaintext in `archguard.yaml`, which is not gitignored.** A new secret gets an environment variable override, not only a YAML field. `LoadConfig` applies `ARCHGUARD_DB_URL`; the API keys are read where `internal/cli` builds the providers.
