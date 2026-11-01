---
title: The pipeline is always written out
status: Proposed
scope:
  - "internal/analysis/**"
  - "internal/cli/**"
  - "internal/config/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# The pipeline is always written out

## Context

A built-in pipeline used when the config has none hides what a check runs, and translating a retired config shape keeps two shapes alive.

## Decision

- **`check` needs the pipeline in the config.** A missing or invalid pipeline exits 3, naming the node or key at fault.
- **A retired pipeline shape gets no aliases.** It is rejected like any invalid pipeline.
- **`check` validates the whole pipeline before it judges any file,** and loading the config never fails because of the pipeline, so other commands keep working on a config only `check` rejects.

## Rules

- A `check` whose pipeline is missing or invalid MUST exit 3.
- A retired pipeline shape MUST be rejected, not converted into nodes.
- Loading the config MUST NOT fail because of the pipeline.
- `check` MUST validate the whole pipeline before it judges any file.

## Alternatives considered

- **Alias retired keys.** Two shapes to document and validate, and the old one hides what runs.
- **A missing pipeline means a built-in default.** Fewer broken configs, but a config that never shows what runs.

## Consequences

- A config without a pipeline fails `check` until one is added.
