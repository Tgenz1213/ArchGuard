---
paths:
  - "docs/arch/**"
  - "docs/ADR_TEMPLATE.md"
---

# ADRs in `docs/arch/`

- ADR files require YAML frontmatter with `title` and `status`; `scope` is optional and may be a single glob string or a list of globs; `rules` (or a `## Rules` body section) is optional too. `status` must appear in `analysis.accepted_statuses` (or use `["*"]`) to be considered.
- ADRs are matched to files structurally via YAML frontmatter `scope` (a glob, matched with `internal/index.MatchGlob`, supporting `**`, applied inside `VectorStore.Search` before the topK similarity cut -- see `docs/arch/0007-scope-filtered-before-topk-similarity.md`; `scope` may also be a YAML list of globs matched with OR semantics, see `docs/arch/0018-multi-pattern-adr-scope.md`) and semantically via embedding similarity — both must pass for an ADR to apply to a given file.
