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

Agent guidance in `CLAUDE.md` and `.claude/rules/` follows `docs/standards/guidance.md`, which says which kind of fact goes where. The page holds the rules; this ADR records the decision to follow it, and its one rule is the part of the page the drift check can judge.

## Rules

- Agent guidance MUST NOT narrate how the code runs: which function calls which, the order of calls, or an inventory of files. An invariant, a footgun or a "when you change X, also change Y" pointer is allowed even when it names code.

## Alternatives considered

- Holding the whole page in review only: a probe on #336 showed the CI model flags call-flow narration in a rules file and stays quiet on a footgun, so this part can be checked by the tool.
- Restating the page's rules here: they would drift apart, and the page could no longer change without amending an accepted ADR.

## Consequences

- Guidance stays short and true, because it holds only what the code cannot say.
- Narration found in a rules file is moved out or deleted rather than updated.
- The rest of the page, such as where a fact belongs, is held in review, because the drift check sees one file's change at a time.
