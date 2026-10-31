---
title: The pipeline is made of nodes
status: Proposed
scope:
  - "internal/analysis/**"
  - "internal/config/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# The pipeline is made of nodes

## Context

Fixed pipeline slots, each with its own short list of scorers, meant every new kind of step needed a new slot, and only one model could judge. This supersedes ADR 0022.

## Decision

- **The pipeline is made of nodes.** Each node has a unique `name` and does one job with one runner.
- **Only the judging node reports violations.** Every other node only chooses which ADRs reach it, or writes fixes for what it reported.
- **A scorer scores and its node decides.** A scorer returns a score for every candidate; the node keeps or drops them.
- **The candidates are the ADRs whose `scope` matches the file,** less those an `archguard-ignore` comment suppresses.
- **A node that asks questions passes on, unasked, an ADR it has no question for,** such as an ADR with no rules when it asks per rule.

## Rules

- Pipeline collaborators such as scorers and nodes MUST be objects with methods, not bare functions.
- A scorer MUST only score its candidates; it MUST NOT drop or reorder them.
- A node other than the judging node MUST NOT decide whether a file violates an ADR.
- An ADR suppressed by an `archguard-ignore: <ADR_ID>` comment MUST be removed before the first node runs.
- A node other than the judging node that asks questions MUST pass on, without asking about it, an ADR it has no question for.

## Alternatives considered

- **Keep fixed slots and add more.** Every new kind of step would need a new slot.
- **Only one kind of runner judges.** Rules out a fully offline pipeline and a cheap judge for literal rules.

## Consequences

- A new runner or job is a new node kind, not a new slot.
- A judging runner that answers without a quote can't use the quote checks built for LLM findings.
