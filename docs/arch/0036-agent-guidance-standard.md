---
title: Agent guidance follows docs/standards/guidance.md
status: Accepted
scope:
  - ".claude/rules/**"
  - "CLAUDE.md"
---

# Agent guidance follows docs/standards/guidance.md

## Context

The rules files in `.claude/rules/` narrated the design: which function calls which, the order a run takes, file inventories and default values. That text duplicated the code and the ADRs, went stale whenever either changed, and cost context without changing what an agent does.

## Decision

Agent guidance in `CLAUDE.md` and `.claude/rules/` follows `docs/standards/guidance.md`, which says which kind of fact goes where. This ADR records that decision and holds no rules beyond it.

## Rules

- Agent guidance MUST NOT narrate how the code runs: which function calls which, the order of calls, or an inventory of files. An invariant, a footgun or a "when you change X, also change Y" pointer is allowed even when it names code.

## Consequences

- Guidance stays short and true, because it holds only what the code cannot say.
- Narration found in a rules file is moved out or deleted rather than updated.
