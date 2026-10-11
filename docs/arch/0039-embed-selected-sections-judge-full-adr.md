---
title: Embed selected ADR sections, judge the full ADR
status: Accepted
scope:
  - "internal/index/**"
  - "internal/analysis/**"
---

# Embed selected ADR sections, judge the full ADR

## Context

Each ADR is embedded whole, so narrative sections such as Context blur its similarity to a changed file (#302), and a long ADR can exceed the embedding model's input limit and never be indexed (#228). Cutting the text at a fixed length was rejected on PR #329, because it silently drops rules or reasoning.

## Decision

- **A project can choose which `##` sections of each ADR are embedded,** by heading, with `analysis.included_headers` or `analysis.exclude_headers`. `included_headers` wins when both are set.
- **The selection changes only the embedding.** The LLM judges each file against the full ADR.
- **The selection is part of the embedding format** (`docs/arch/0038-embedding-format-saved-with-the-index.md`), so changing it embeds every ADR again.
- **An ADR left with no text to embed is reported and not indexed,** never embedded empty.

## Rules

- The text sent to the LLM for judging MUST be the ADR's full content, never the embedded selection.
- An ADR whose selected sections are empty MUST NOT be embedded.

## Alternatives considered

- **Truncating an ADR to the model's input limit.** Rejected on PR #329: it silently drops rules or reasoning.
- **Embedding only the ADR's rules (#302's spike).** Narrower, but an ADR without rules would need a fallback, and a project could not choose other sections.

## Consequences

- A project can keep narrative out of matching without rewriting its ADRs.
- A selection that names no section of an ADR drops that ADR from the index until the selection or the ADR changes; `archguard index` lists it.
- Selection works on `##` headings, so it suits ADRs written in Markdown with consistent headings, such as those that follow `docs/ADR_TEMPLATE.md`.
