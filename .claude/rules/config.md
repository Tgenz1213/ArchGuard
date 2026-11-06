---
paths:
  - "internal/config/**"
  - "archguard.yaml"
---

# `internal/config`

- **Validate `analysis.pipeline` and the `jev` block in `ParsePipeline`, never in `LoadConfig`,** so `index` and other commands still run on a pipeline only `check` rejects (`docs/arch/0051-the-pipeline-is-always-written-out.md`).
- **A new job, runner or setting is added to `keysForJob`, `runnersForJob` or the presets here as well as to `analysis.BuildPipeline`,** because a key a job doesn't list is rejected as not allowed on the node.
- **Read `max_relevant_adrs` through `Analysis.RelevantADRLimit()`,** never the field, so its default applies in one place.
- **`analysis.confluence.token` is plaintext in `archguard.yaml`, which is not gitignored.** A new secret gets an environment variable override, not only a YAML field. `LoadConfig` applies `ARCHGUARD_DB_URL`; `check` reads `ARCHGUARD_JEV_API_KEY` (required only when a node uses Jev), and the other API keys are read where `internal/cli` builds the providers.
