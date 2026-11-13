---
name: review-local
description: Run seven specialised reviewers and blind verifiers on the local diff and print a report. Forms are /review-local (staged, unstaged and untracked), /review-local --staged, and /review-local <ref>.
disable-model-invocation: true
---

# Review the local diff

Follow `.claude/review/pipeline.md` for the whole procedure. This page covers only what is specific to the local forms.

## Input

| Form | Reviews |
|---|---|
| no argument | staged, unstaged and untracked changes |
| `--staged` | staged changes only; untracked files are excluded |
| `<ref>` | `<ref>...HEAD` |

If the diff is empty, say "nothing to review" and stop. Dispatch no agent.

## Specific to this skill

- The ticket comes from a `feat/<N>-slug` or `fix/<N>-slug` branch name, read with `gh issue view <N>`. Any other branch name means no ticket.
- Rules come from a trusted commit, never the working tree: `HEAD` for the default and `--staged` forms, `git merge-base <ref> HEAD` for the `<ref>` form. Read each file with `git show <commit>:<path>`.
- Probers may run without asking, because the code under review is the user's own.
- Dispatch only the `reviewer` and `prober` agents the role files name, never a general-purpose agent.
- The output is the report in the terminal. Write nothing to the repo.
