---
title: Probers run code in a worktree without committing
status: Accepted
scope:
  - ".claude/agents/**"
  - ".claude/hooks/prober-bash.sh"
  - ".claude/agents/prober.md"
  - ".claude/skills/review-*/**"
  - ".claude/review/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Probers run code in a worktree without committing

## Context

A `reviewer` can't run tests, builds or mutations, and a `writer` can commit. Review roles that need to execute code had no agent that is safe to dispatch. This supersedes ADR 0040.

## Decision

Subagents are dispatched as one of the committed agents in `.claude/agents/`:

- `writer` sets `isolation: worktree`, so it edits and commits in its own worktree on its own branch.
- `reviewer` has no Edit or Write tool, and a `PreToolUse` hook on Bash, `.claude/hooks/read-only-git.sh`, lets through only read-only git commands.
- `prober` sets `isolation: worktree` and can edit and write there, so it can run tests and throwaway reproductions. A `PreToolUse` hook on Bash, `.claude/hooks/prober-bash.sh`, lets through only an allowlist of single commands: go build, test, vet, run and list, golangci-lint run, read-only git, git apply and git restore, `git checkout --detach` of `refs/review/pr-N` or a 7 to 40 character hex SHA, cleaning its own worktree, and `gh pr view`, `gh pr diff` and `gh issue view`. It commits nothing and leaves nothing to merge.

Review skills dispatch their subagents as `reviewer` or `prober`.

## Rules

- An agent in `.claude/agents/` whose tools include Edit or Write, or that lists no `tools`, MUST set `isolation: worktree`.
- An agent in `.claude/agents/` whose `tools` include Bash but not Edit or Write MUST run `.claude/hooks/read-only-git.sh` as a `PreToolUse` hook on Bash.
- `read-only-git.sh` MUST exit 2 for any command it does not explicitly allow, including one it cannot parse.
- The `prober` agent MUST set `isolation: worktree`.
- The `prober` agent MUST run `.claude/hooks/prober-bash.sh` as a `PreToolUse` hook on Bash.
- `prober-bash.sh` MUST exit 2 for any command it does not explicitly allow, including `git commit`, `git push`, a branch change and a `gh` command that writes.
- A review skill in `.claude/skills/review-*/` MUST dispatch its subagents as `reviewer` or `prober`.

## Alternatives considered

- **A `writer` that commits nothing.** Nothing mechanical stops it from committing or pushing.
- **`Bash(<pattern>)` in `tools`.** Spike #380 showed it does not narrow Bash.
- **Running these roles in the session itself.** It spends the session's context and works in the session's checkout.

## Consequences

- A prober has a cold cache in each worktree and shares a writable module cache with the session.
- The hook limits what a command can write, and the worktree keeps the agent's own git changes off the session's checkout. Code a test runs can still write anywhere the user can, which is why PR code needs the user's approval.
- A review role that must execute code has an agent built for it, and reviewers stay read-only.
