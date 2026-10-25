---
title: Triage agents read GitHub through read-only tools and gh commands
status: Superseded
scope:
  - ".claude/agents/**"
  - ".claude/hooks/read-only-git.sh"
  - ".claude/workflows/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Triage agents read GitHub through read-only tools and gh commands

## Context

The `triage-issues` workflow (#396) runs one agent per issue, and each agent reads issues and pull requests. In spike #390 those agents were ordinary workflow agents: they had GitHub write tools, and only the prompt kept them read-only. ADRs 0040 and 0041 rejected prose as the guard for subagents. The `reviewer` agent can't read GitHub at all. In a cloud session, GitHub comes through the connector's MCP tools. On Windows, the spike's agents used `gh`.

## Decision

Workflow agents that read GitHub run as the `triager` agent. It is read-only under ADRs 0040 and 0041: it names only GitHub MCP tools that can't write, and its Bash calls go through `.claude/hooks/read-only-git.sh`. That hook also allows `gh issue view`, `gh issue list`, `gh pr view`, `gh pr list` and `gh pr diff`, under the same no-operator rules as its git commands, so that machines without the GitHub connector can still read. Because the `reviewer` agent shares the hook, it can run them too, and none of them can write.

## Rules

- `read-only-git.sh` MUST NOT allow a `gh` command that can change GitHub, including `gh api`.
- An `agent()` call in `.claude/workflows/` that reads GitHub MUST set `agentType: 'triager'`.

## Alternatives considered

- **Ordinary workflow agents, as in the spike.** Read-only would depend on the prompt alone, which ADR 0040 rejected.
- **The session fetches the issues and passes them in `args` to `reviewer` agents.** It is read-only, but `/triage-issues` would only work after the session fetches first, and the agents couldn't follow links to other issues or PRs.
- **A separate hook for `gh`.** Hooks run together and any one of them can refuse, so a second hook can't allow what `read-only-git.sh` refuses.

## Consequences

- `/triage-issues` can't edit, comment on or close an issue. Approved edits are applied afterwards, by hand or with the `file-ticket` skill.
- The `reviewer` agent can read issues and pull requests with `gh`.
- A local GitHub connector registers its tools under other names. Those names aren't listed, so on such a machine the agents use `gh`.
