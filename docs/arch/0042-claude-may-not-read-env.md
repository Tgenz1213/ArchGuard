---
title: Claude may not read .env files
status: Accepted
scope:
  - ".claude/settings.json"
  - ".claude/hooks/block-env.sh"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Claude may not read .env files

## Context

`.env` holds provider API keys, which the CLI loads at startup. Nothing stopped Claude from reading it. In spike #385 a `writer` subagent opened the main checkout's `.env` with the Read tool, and the key went into its transcript. A test with a canary `.env` showed that relative `Read(.env)` deny rules miss two cases: a subagent in a worktree reading the main checkout's file by absolute path, and commands Claude Code doesn't recognize as file reads, such as `python3 -c "open('.env')"`.

## Decision

- `.claude/settings.json` denies `Read(//**/.env)` and `Read(//**/.env.*)`. The `//` anchors them at the filesystem root, so they hold in every worktree and for absolute paths.
- A `PreToolUse` hook on Bash, `.claude/hooks/block-env.sh`, refuses any command whose hook input names `.env` or `.env.<suffix>`.

A recursive search that never names the file, such as `grep -r KEY .`, still reads it. Only the OS sandbox closes that, and the sandbox doesn't run on native Windows. So API keys are best kept in environment variables (`ARCHGUARD_API_KEY`, `ARCHGUARD_EMBEDDING_API_KEY`) rather than in `.env`.

## Rules

- `.claude/settings.json` MUST list `Read(//**/.env)` and `Read(//**/.env.*)` under `permissions.deny`.
- `.claude/settings.json` MUST run `.claude/hooks/block-env.sh` as a `PreToolUse` hook on Bash.
- `block-env.sh` MUST exit 2 for any hook input that names `.env` or `.env.<suffix>`.

## Alternatives considered

- **Relative `Read(.env)` rules only.** The canary test showed they miss worktree subagents and script one-liners.
- **The sandbox's read deny list.** It blocks every process, but not on native Windows.
- **Dropping `.env` support from the CLI.** It is a user-facing feature, and the keys would still be readable wherever users put them.

## Consequences

- Claude can't read, edit or create `.env`, `.env.local` or `.env.example`; a person does that.
- A Bash command that only mentions `.env`, such as `git check-ignore .env`, is refused too, and so is one whose description mentions it.
