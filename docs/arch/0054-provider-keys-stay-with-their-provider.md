---
title: Model provider keys come from the environment and go only to their host
status: Proposed
scope:
  - "internal/cli/**"
  - "internal/config/**"
  - "internal/inference/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Model provider keys come from the environment and go only to their host

## Context

One check can call several model providers, the services that embed, ask or judge, on several hosts. `archguard.yaml` is committed, and a key sent to the wrong host is a leak.

## Decision

- **Every model provider key comes from an environment variable,** never from `archguard.yaml`.
- **A key goes only to the host it is configured for.** No provider falls back to a key configured for another host.

## Rules

- A model provider key MUST come from an environment variable, never from `archguard.yaml`.
- A model provider key MUST NOT be sent to any host other than the one it is configured for.

## Alternatives considered

- **One shared key variable.** A key would reach every host in the pipeline.
- **Keys in `archguard.yaml` with a warning.** The file is committed.

## Consequences

- A pipeline that calls several hosts with keys needs one environment variable per key.
