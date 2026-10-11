---
paths:
  - "docs/arch/**"
  - "docs/ADR_TEMPLATE.md"
---

# ADRs in `docs/arch/`

- ADR files require YAML frontmatter with `title` and `status`; `scope` (one glob or a list), `similarity_threshold` and `rules` (or a `## Rules` body section) are optional. `status` must appear in `analysis.accepted_statuses` (or use `["*"]`) to be considered.
- An ADR applies to a file only when its `scope` matches and its embedding similarity passes the threshold (`docs/arch/0007-scope-filtered-before-topk-similarity.md`, `docs/arch/0018-multi-pattern-adr-scope.md`).
- This repo's own ADRs are checked by ArchGuard in CI, so a `## Rules` section here becomes rule statements (`docs/arch/0023-adr-screening-rules.md`).
