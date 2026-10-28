---
paths:
  - "internal/config/**"
  - "archguard.yaml"
---

# `internal/config`

- **A new scorer is added to `scorerNames` and to `rankingScorers` or `screeningScorers` here, as well as to package `stage`,** because `config` cannot import `stage` and rejects an unknown name, or a scorer in a stage it can't fill, when it loads (`docs/arch/0022-candidate-scoring-pipeline.md`).
- **Read `max_relevant_adrs` through `Analysis.RelevantADRLimit()`,** never the field, so its default applies in one place.
- **`analysis.confluence.token` is plaintext in `archguard.yaml`, which is not gitignored.** A new secret gets an environment variable override, not only a YAML field. `LoadConfig` applies `ARCHGUARD_DB_URL` and `ARCHGUARD_JEV_API_KEY` (required only when a stage uses Jev); the other API keys are read where `internal/cli` builds the providers.
