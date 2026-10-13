---
title: Read-only subagents get no MCP tools
status: Accepted
scope:
  - ".claude/agents/**"
---

# Read-only subagents get no MCP tools

## Context

ADR 0040 makes the `reviewer` agent read-only by giving it no Edit or Write tool and running its Bash calls through `.claude/hooks/read-only-git.sh`. That hook sees only Bash. A subagent can also inherit the session's MCP tools, and some of them write files: Serena, which some machines register, creates and edits files. In the Windows check for PR #384 the reviewer listed Serena's tools as its own. The Claude Code docs say a `tools` allowlist already drops MCP tools, but nothing in the repo held a build to that.

## Decision

An agent that may not change the checkout denies every MCP tool by name with `disallowedTools: mcp__*`, in addition to its `tools` allowlist. `test/agents` checks the frontmatter of every agent in `.claude/agents/` against this rule and ADR 0040's.

## Rules

- An agent in `.claude/agents/` that lists `tools` without Edit or Write MUST set `disallowedTools: mcp__*`.

## Alternatives considered

- **Rely on the `tools` allowlist alone.** It holds only while every Claude Code build treats an allowlist as excluding MCP tools, and the Windows check left that in doubt.
- **A `PreToolUse` hook on `mcp__.*`.** It works only if subagent hooks see MCP calls, which the docs don't say, and it is more code than one frontmatter line.

## Consequences

- A reviewer can't use any MCP tool, read-only ones included; a task that needs one goes to a `writer`.
- A new read-only agent fails `go test ./test/...` until it sets the field.
