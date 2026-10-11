---
title: ADR scope frontmatter accepts a single glob or a list of globs
status: Accepted
scope: "internal/index/**"
---

# ADR scope frontmatter accepts a single glob or a list of globs

## Context

An ADR's `scope` was a single glob. An ADR that applies to several unrelated path shapes had to use one overly broad glob, or be duplicated under several IDs, which splits its baseline and suppression history. See #171.

## Decision

`scope` accepts either a single glob string, with the same meaning as before, or a YAML list of globs. A file is in scope when any pattern matches it. An empty scope is unrestricted.

Persistence stays schema-free for both vector stores:

- The local JSON index writes a single pattern as a bare string, the same bytes as the old format, and several patterns as an array. Existing `.archguard/index.json` files load without migration.
- Postgres keeps the existing `scope` TEXT column, with no `ALTER`. A single pattern is stored as raw text, as before; several are stored as a JSON array string in the same column.

## Rules

- `scope` MUST accept either a single glob string or a YAML list of globs.
- A file MUST be in scope when any pattern matches it, and an empty scope MUST be unrestricted.
- The local JSON index MUST write a single pattern as a bare string and several patterns as an array.
- Postgres MUST keep the existing `scope` TEXT column with no `ALTER`, storing a single pattern as raw text and several as a JSON array string.

## Alternatives considered

None recorded.

## Consequences

- An ADR author can write `scope: ["a/**", "b/**"]`, or the block-list form, to apply one ADR to unrelated paths without over-broadening a glob or duplicating the ADR.
- No regression: every existing single-string `scope`, in an ADR file, a local index or a Postgres row, parses the same as before.
