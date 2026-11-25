---
title: An asking runner is named after the protocol it speaks
status: Accepted
scope:
  - "internal/config/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# An asking runner is named after the protocol it speaks

## Context

Several servers answer the same question API: a hosted model, and open-weight models a user runs locally. A runner named after one of them needs a new name, and a copy of the same client, for each other server that speaks the API.

## Decision

A runner that asks questions is named after the API it speaks. Which server it calls and which model answers are settings on the node, with defaults in one top-level block.

## Rules

- A runner that asks questions MUST be named after the API it speaks, never after a product or a model.
- The server and the model a node asks MUST be node settings, not part of the runner's name.

## Alternatives considered

- **One runner per product.** Every new server that speaks the API would need its own runner and client.
- **Keeping the product's runner as an alias.** Two names for one runner, with no released config that needs the old one.

## Consequences

- One client serves every server that speaks the API, so a local server needs only a different server setting.
- The runner's name doesn't say which model answers; the node's settings do.
