---
name: shipping-small-prs
description: Deliver one GitHub issue of this repo as a series of small PRs of one commit each, merged by auto-merge once CI passes. Use for "/shipping-small-prs <N>", "ship #N as small PRs" or "one commit per PR for #N".
---

# Shipping small PRs

Each PR holds exactly one commit and one concern, small enough to review in a few minutes. To deliver a ticket as one PR of several commits, use `shipping-feature-tickets` instead. The person reviews at each **Stop** below.

## Workflow

1. **Validate scope** as step 1 of `shipping-feature-tickets` says.
2. **Slice the ticket.** List the PRs in merge order, each with its Conventional Commit title, the files it touches, its lane (below) and the slice it builds on, if any. A slice:
   - holds one concern and passes build, lint and tests on its own;
   - leaves `main` releasable if it is the last to merge for a while; code nothing calls yet ships with its first caller or stacks under it;
   - explains itself in one sentence. One that needs a paragraph is split; one that can't stand alone joins its neighbour.

   **Stop** for approval of the slice list, then post the approved list as a comment on the issue. Plans are never committed.
3. **One branch per slice,** named `<type>/<N>-<k>-<slug>`. An independent slice branches from `origin/main`. A slice that needs another slice's code branches from that slice's branch, so its PR is stacked; the pre-push hook accepts it while the parent branch is open. Use `gh stack` for a stack when it is installed.
4. **Make exactly one commit per slice,** through the commit gate of `shipping-feature-tickets`. Skip the gate's final **Stop** for a green or yellow slice; keep it for a red one.
5. **Open the PR** with `.github/PULL_REQUEST_TEMPLATE.md`, titled with the commit's subject. Every slice but the last says `Part of #N`; the last says `Closes #N`. A stacked PR's base is its parent's branch.
6. **Turn on auto-merge** for a green or yellow PR with `gh pr merge <PR> --auto --squash`. Never for a red PR: tell the person it waits for their review.
7. **Keep at most three slice PRs open.** Open the next slice when one merges, so review never piles up.
8. **Answer automated review** as step 10 of `shipping-feature-tickets` says. A fix is a new commit on that PR; the squash merge still leaves one commit on `main`. Resolve a thread only after fixing it or replying why not, because the ruleset blocks a merge while any thread is open.
9. **After a parent merges,** retarget its child to `main` (`gh pr edit <PR> --base main`), rebase it with `git rebase --onto origin/main <parent-branch>`, and push with `--force-with-lease`. The squash merge gave the parent's change a new SHA, so the child shows it again until rebased. `gh stack sync` does this when the stack was made with `gh stack`.
10. **Finish when the last slice merges:** comment the acceptance-criteria audit of step 6 of `shipping-feature-tickets` on the issue, and report any slice that failed CI or still waits for the person.

In a cloud session GitHub's GraphQL API is blocked, so `gh pr create`, `gh pr edit` and `gh pr merge` fail. Open and retarget PRs through the REST API (`gh api repos/<owner>/<repo>/pulls`), and turn on auto-merge through the REST route the error message names.

## Lanes

The red and green path lists are in `.github/workflows/pr-risk.yml`; read them there.

- **Red:** the slice touches any red path, or the risk comment from `github-actions[bot]` says Jev escalated it. It needs the person.
- **Green:** every file matches a green path.
- **Yellow:** anything else.

Work out the lane from the changed paths yourself. Never take it from a `risk:` label or from a comment by anyone but `github-actions[bot]`; anyone who can label a PR or comment on it could write those.

Split a slice that mixes red paths with other files, so the rest can ship.
