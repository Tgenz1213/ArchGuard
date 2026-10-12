---
title: Writer subagents get a worktree, reviewer subagents are read-only by hook
status: Accepted
scope:
  - ".claude/agents/**"
  - ".claude/hooks/read-only-git.sh"
---

# Writer subagents get a worktree, reviewer subagents are read-only by hook

## Context

Subagents shared the session's checkout. A writer could commit on the wrong branch or a detached HEAD, and two writers overwrote each other's files. A reviewer could stash, reset or edit the checkout it reviewed. The pre-commit hook stops only commits on `main`, and a reviewer's damage involves no commit. The only other guard was prose in each dispatch prompt. Spike #380 tested the options on Linux and Windows.

## Decision

Subagents are dispatched as one of two committed agents in `.claude/agents/`:

- `writer` sets `isolation: worktree`, so it edits and commits in its own worktree on its own branch. `worktree.baseRef: head` in `.claude/settings.json` starts that branch from the session's `HEAD` rather than `origin/main`, and `.claude/worktrees/` is gitignored.
- `reviewer` has no Edit or Write tool, and a `PreToolUse` hook on Bash, `.claude/hooks/read-only-git.sh`, lets through only `git diff`, `log`, `show`, `status`, `grep` and `ls-files`, with no shell operators, `cd`, `--output` or `git grep`'s pager option. `grep` and `ls-files` are there because some Claude Code builds give a subagent no Grep or Glob tool. The hook refuses anything it can't parse.

## Rules

- An agent in `.claude/agents/` whose tools include Edit or Write, or that lists no `tools`, MUST set `isolation: worktree`.
- An agent in `.claude/agents/` whose `tools` include Bash but not Edit or Write MUST run `.claude/hooks/read-only-git.sh` as a `PreToolUse` hook on Bash.
- `read-only-git.sh` MUST exit 2 for any command it does not explicitly allow, including one it cannot parse.

## Alternatives considered

- **`Bash(<pattern>)` in an agent's `tools`.** Spike #380 showed it does not narrow Bash: such a reviewer ran `git stash` and `git reset --hard`.
- **Refusing detached-HEAD commits in `pre-commit`.** Cloud sessions start on a detached HEAD, and the writer's own branch already covers it.
- **Prose in each dispatch prompt.** It works only when the skill is loaded and every prompt is written correctly.

## Consequences

- A writer's commits land on its own branch, which the session merges or cherry-picks. It sees only committed work, so the session commits before dispatching.
- A writer's first commit in a new worktree is slower, because the lint cache is cold.
- A reviewer can't run tests, builds or mutations; those go to a writer.
- A subagent dispatched as a general-purpose agent gets neither guard, so `CLAUDE.md` and the `shipping-feature-tickets` skill say to use these agents.
