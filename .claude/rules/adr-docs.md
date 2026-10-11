---
paths:
  - "docs/arch/**"
  - "docs/ADR_TEMPLATE.md"
---

# ADRs in `docs/arch/`

- An ADR is short and records a decision. Once accepted it is never edited; a changed decision gets a new ADR that supersedes it, and a change in behaviour needs no ADR edit ([guidance.md](../../docs/standards/guidance.md)).
- **CI checks this repo against its own ADRs.** A `## Rules` bullet becomes a rule the model screens every file in the ADR's `scope` against, so write each one as a MUST or MUST NOT statement.
- **An ADR whose `status` is not in `analysis.accepted_statuses` is silently ignored.** A superseded ADR stops being checked once its status changes.
