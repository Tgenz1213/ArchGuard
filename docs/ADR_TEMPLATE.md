---
title: "[Short, Descriptive Title]"
status: "[Accepted | Proposed | Superseded]"
scope: "[Glob pattern, e.g., **/*.go -- or a YAML list of globs, matched with OR semantics]"
# similarity_threshold: 0.65   # Optional: overrides vector_store.similarity_threshold for this ADR only
# rules:                       # Optional: used instead of the ## Rules section below when set
#   - All code MUST be written in Go.
#   - statement: Handlers MUST NOT import the database package directly
#     violating: ['import "app/db"']
#     compliant: ["call the repository interface"]
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# [ADR Title]

## Context

[Describe the problem or context that requires a decision.]

## Decision

[Clearly state the decision and any rules or constraints it imposes.]

## Rules

[One bullet per rule, each a MUST or MUST NOT statement that restates the Decision. Every file in `scope` is checked against each rule. Nested bullets are ignored. Write "None." when no file could break the decision.]

- [e.g. All code MUST be written in Go.]

## Alternatives considered

[The options rejected and why. Write "None recorded." when there were none.]

- **[Alternative].** [Why it was rejected.]

## Consequences

[Describe the expected outcomes, both positive and negative.]
