---
title: Pipeline nodes run in list order
status: Accepted
scope:
  - "internal/config/**"
  - "internal/analysis/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Pipeline nodes run in list order

## Context

Nodes (`docs/arch/0049-pipeline-nodes.md`) need an order.

## Decision

- **The pipeline is a list, run top to bottom.** Each node gets the ADRs the node above it kept.
- **The first node narrows the candidates,** so no later node asks about every scope-matched ADR.
- **Exactly one node judges.** Only fix suggestions may come after it.

## Rules

- Pipeline validation MUST reject a first node that does not limit how many ADRs it passes on.
- Pipeline validation MUST reject a pipeline with no judging node, more than one, or any node after it other than one that suggests fixes.

## Alternatives considered

- **A graph where each node names its inputs.** It needs routing, cycle checks, merging of disagreeing judges and per-path reporting.

## Consequences

- Every ADR takes the same path through the nodes.
- A config can't send some ADRs to one judge and the rest to another.
