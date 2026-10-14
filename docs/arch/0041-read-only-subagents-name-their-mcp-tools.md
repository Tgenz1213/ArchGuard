---
title: Read-only subagents get only the MCP tools they name
status: Accepted
scope:
  - ".claude/agents/**"
---

# Read-only subagents get only the MCP tools they name

## Context

ADR 0040 makes the `reviewer` agent read-only by giving it no Edit or Write tool and running its Bash calls through `.claude/hooks/read-only-git.sh`. That hook sees only Bash. A subagent can also get MCP tools, and some of them write files: Serena, which some machines register, creates and edits files. Probes for #388 on Windows with Serena connected and in a cloud session found that a `tools` allowlist already keeps every unlisted MCP tool from the reviewer, as the Claude Code docs say. Some MCP tools are useful for review and can't write, such as Context7's documentation lookup.

## Decision

A read-only agent gets an MCP tool only by naming that one tool in its `tools` list, and only when the tool can't write. A server pattern such as `mcp__<server>` or `mcp__<server>__*` is never listed, since it would also grant whatever write tools the server has or adds. The tool's full name depends on how the server is registered, so an agent lists each name the tool is known by; a name that doesn't resolve on a machine is ignored there. `test/agents` holds the known read-only MCP tools and checks every agent in `.claude/agents/` against this rule and ADR 0040's.

## Rules

- An agent in `.claude/agents/` that lists `tools` without Edit or Write MUST NOT list an MCP server pattern, and every `mcp__` tool it lists MUST be one that cannot write files.

## Alternatives considered

- **`disallowedTools: mcp__*`.** It removes every MCP tool, including the read-only ones worth having, and Claude Code applies it before `tools`, so no MCP tool can be listed back.
- **Deny write-capable servers by name, such as `mcp__serena`.** Any server added later would reach the reviewer, and one server can mix read and write tools.
- **A `PreToolUse` hook on `mcp__.*`.** It works only if subagent hooks see MCP calls, which the docs don't say, and it is more code than a list.

## Consequences

- The reviewer can look up library docs with Context7 but can't use any other MCP tool.
- Adding an MCP tool to a read-only agent means adding it to the test's list of read-only tools, which puts the "can it write?" question in front of a reviewer.
- If a Claude Code build stops treating `tools` as an allowlist for MCP tools, nothing here catches it; the #388 probes are the check to repeat.
