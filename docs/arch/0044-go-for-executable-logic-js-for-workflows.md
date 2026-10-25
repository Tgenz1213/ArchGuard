---
title: Use Go for executable logic, and JavaScript only for Claude Code workflows
status: Accepted
scope: "**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Use Go for executable logic, and JavaScript only for Claude Code workflows

## Context

ADR 0001 made Go the only language for executable logic, with shell and PowerShell allowed for automation. Claude Code runs a saved workflow only from a JavaScript file in `.claude/workflows/`, so the `triage-issues` workflow (#396) can't be written in Go. This supersedes ADR 0001.

## Decision

1. **Mandatory Language**: All executable logic is implemented in **Go**.
2. **Permitted Non-Go Files**:
   - Configuration: `.yaml`, `.yml`, `.json`, `.toml`
   - Automation: `.sh`, `.ps1`, and `.js` only under `.claude/workflows/`
   - Documentation: `.md`
3. **Out of Scope**: This ADR does not dictate how Go is written. It only requires that the language is Go.

## Rules

- All executable logic MUST be implemented in Go, except shell or PowerShell automation and Claude Code workflow scripts in `.claude/workflows/`.
- A JavaScript file MUST NOT exist outside `.claude/workflows/`.

## Alternatives considered

- **Exclude `.claude/workflows/` from the drift check.** The workflows would then be checked against no ADR, including ADR 0043's rule for them.
- **An `archguard-ignore: 0001` comment in each workflow.** Every new workflow would need the same exemption.

## Consequences

- Claude Code workflows can be committed and are still checked against every other ADR.
- Workflow scripts don't go through the Go compiler or the Go tests, so they rely on review.
