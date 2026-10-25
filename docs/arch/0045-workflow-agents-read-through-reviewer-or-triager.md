---
title: Workflow agents read the repo as reviewer and GitHub as triager
status: Accepted
scope:
  - ".claude/agents/**"
  - ".claude/hooks/read-only-git.sh"
  - ".claude/workflows/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Workflow agents read the repo as reviewer and GitHub as triager

## Context

ADR 0043 made workflow agents that read GitHub run as `triager`. It said nothing about workflow agents that read only the repo, such as the `audit-standards` workflow (#418), which checks Go code against `docs/standards/`. The drift check read 0043's rule as "every workflow agent must be `triager`" and flagged the audit's `reviewer` agents three times. Running them as `triager` would give a code review an agent built for triage. This supersedes ADR 0043.

## Decision

Every agent in a workflow runs as a read-only agent from `.claude/agents/`, picked by what it reads:

- An agent that reads only the repo runs as `reviewer`.
- An agent that reads GitHub runs as `triager`. `triager` names only GitHub MCP tools that can't write, and its Bash calls go through `.claude/hooks/read-only-git.sh`. That hook also allows `gh issue view`, `gh issue list`, `gh pr view`, `gh pr list` and `gh pr diff` under the same no-operator rules as its git commands, so machines without the GitHub connector can still read.

## Rules

- Every `agent()` call in `.claude/workflows/` MUST set `agentType` to `'reviewer'` or `'triager'`.
- An `agent()` call in `.claude/workflows/` that reads GitHub issues or pull requests MUST set `agentType: 'triager'`.
- An `agent()` call in `.claude/workflows/` that reads only the repo MUST set `agentType: 'reviewer'`.
- `read-only-git.sh` MUST NOT allow a `gh` command that can change GitHub, including `gh api`.

## Alternatives considered

- **Every workflow agent runs as `triager`.** It passes the check, but it gives repo-only work GitHub access and blurs what the agent is for.
- **A separate `auditor` agent.** It would copy `reviewer` and would still need a rule that names it.
- **An `archguard-ignore: 0043` comment in each repo-only workflow.** Every new workflow would need the same exemption.

## Consequences

- A workflow that reviews code and one that triages issues each use the agent built for that job.
- A workflow agent with no `agentType` breaks a rule, so it can't fall back to the default workflow agent, which can write.
