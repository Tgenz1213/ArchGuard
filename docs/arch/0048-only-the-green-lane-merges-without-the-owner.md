---
title: Only the green lane merges without the owner
status: Accepted
scope:
  - ".github/workflows/**"
  - ".claude/skills/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Only the green lane merges without the owner

## Context

Agents open most PRs here, and the owner's review is the bottleneck. `pr-risk.yml` sorts each PR into a lane: red if it touches a red path, green if every file is on the green list, yellow otherwise, and Jev can raise any PR to red. #440 was labelled red but merged anyway, because an agent following a skill turned on auto-merge itself: a lane written only in prose is not a gate.

## Decision

The PR Risk workflow is the only thing that lets a PR merge without the owner's review. For a green PR it has a separate GitHub App, the gate, approve the classified commit and turns on auto-merge; for any other PR it dismisses the gate's approval and turns auto-merge off. Jev can move a PR to red but never to a lower lane. The workflow runs on `pull_request_target`, so the copy on `main` decides, and it never checks out or runs the PR's code, so the gate's key can't leak to it.

## Rules

- A workflow other than `pr-risk.yml`, and a skill, MUST NOT turn on auto-merge, merge a pull request, or submit an approving review.
- `pr-risk.yml` MUST approve a pull request or turn on auto-merge only when every changed file is on its green path list and Jev did not escalate it.
- `pr-risk.yml` MUST NOT check out or run a pull request's code.
- Jev's answer MUST NOT move a pull request to a lower-risk lane.

## Alternatives considered

- **Lanes enforced by skill instructions.** An agent misread them and merged a red PR (#440).
- **Required approvals with the owner approving every green PR.** Keeps the bottleneck this is meant to remove.
- **The workflow's own `GITHUB_TOKEN` approving.** A review by `github-actions[bot]` counts only through a repository setting that also lets any workflow approve, so a PR could add a workflow that approves itself.

## Consequences

- A green PR merges on CI alone; yellow and red wait for the owner, who keeps the ruleset bypass.
- The gate's private key lives in repository secrets that `pull_request_target` exposes, so a step that checks out PR code would leak it.
- While Jev's confidence is low, most PRs escalate to red and few auto-merge.
