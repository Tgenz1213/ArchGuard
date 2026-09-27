---
title: "[Short, Descriptive Title]"
status: "[Accepted | Proposed | Superseded]"
scope: "[Optional: glob pattern, e.g., **/*.go -- or a YAML list of globs, matched with OR semantics]"
# similarity_threshold: 0.65   # Optional: overrides vector_store.similarity_threshold for this ADR only
# rules:                       # Optional: used instead of the ## Rules section below when set
#   - All code MUST be written in Go.
#   - statement: Handlers MUST NOT import the database package directly
#     violating: ['import "app/db"']
#     compliant: ["call the repository interface"]
---

# [ADR Title]

## Context

[Describe the problem or context that requires a decision.]

## Decision

[Clearly state the decision and any rules or constraints it imposes.]

## Rules

[Optional. One bullet per rule, each a short statement of what violating code looks like. Nested bullets are ignored.]

- [e.g. All code MUST be written in Go.]

## Consequences

[Describe the expected outcomes, both positive and negative.]
