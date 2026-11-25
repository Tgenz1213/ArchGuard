---
paths:
  - "internal/config/**"
  - "archguard.yaml"
---

# `internal/config`

- **Validate `analysis.pipeline` and the `system_one` block in `ParsePipeline`, never in `LoadConfig`,** so `index` and other commands still run on a pipeline only `check` rejects (`docs/arch/0051-the-pipeline-is-always-written-out.md`).
- **A new job, runner or setting is added to `keysForJob`, `runnersForJob` or the presets here as well as to `analysis.BuildPipeline`,** because a key a job doesn't list is rejected as not allowed on the node.
- **Read `max_relevant_adrs` through `Analysis.RelevantADRLimit()`,** never the field, so its default applies in one place.
- **`analysis.confluence.token` is plaintext in `archguard.yaml`, which is not gitignored.** A new secret gets an environment variable override, not only a YAML field. `LoadConfig` applies `ARCHGUARD_DB_URL`; `internal/cli` reads `ARCHGUARD_SYSTEM_ONE_KEY_ORIGIN` before it loads `.env` and ignores a `.env` value for it, so neither `archguard.yaml` nor a committed `.env` can redirect a CI secret; `ARCHGUARD_SYSTEM_ONE_API_KEY` and the other API keys may come from `.env` and are read where `internal/cli` builds the providers.
