---
name: shipping-feature-tickets
description: Deliver one GitHub issue of this repo end to end, from scope check to an open PR with green CI. Use for "/shipping-feature-tickets <N>", "ship #N", "do a PR for #N" or "let's knock out #N".
---

# Shipping feature tickets

Not for vague or exploratory asks; this starts once there is a concrete ticket. The person reviews at each **Stop** below unless they waived it for that change.

## Workflow

1. **Validate scope.** Read the issue, its comments and its parent epic if it says "Part of #M"; the epic defines its terms. In a cloud session `gh` is not installed, so use the GitHub connector. Stop and ask if the issue is closed, superseded, a discussion, or lacks pass/fail acceptance criteria.
2. **One branch for the whole ticket.** Locally, a worktree on `feat/<N>-<slug>`; in a cloud session, the branch the session was given. Preparatory refactors and comment audits are commits on that branch, never side branches. Run the tests before changing anything, so a failure you didn't cause is known.
3. **Plan and surface decisions.** Tell the person the intended changes and the commit sequence, one concern each. A conflict between acceptance criteria, or a change to observable behaviour, goes to them as options before any code; record what they accept for the PR body. **Stop** for approval. Plans are never committed.
4. **Commit one concern at a time,** each through the gate below.
5. **Review the whole branch** with `reviewer` agents in parallel, split by angle (behaviour vs. base, design and docs, correctness and tests). Give each a scope and a budget of about 20 minutes, and ask for partial findings when it runs out. Require evidence, not reading:
   - **Differential:** `git archive <base>` into scratch, run the same fixtures through both trees, and diff output, exit codes and cache keys. Never claim "identical" without it.
   - **Mutation:** in a copy of the repo, break each behaviour a new test names; a mutation no test catches is a test gap.
   - A `reviewer` can't run these. Give each run to a `writer` agent, which works in its own worktree and commits nothing.
   - **External resources:** start Docker, the database or the service a claim depends on and run it. If it can't run here (Docker is often missing in a cloud session), the claim is unverified, not passed.
6. **Fix findings in the right commit, then audit the acceptance criteria:** a table of each criterion, its verdict and the test or run that proves it, with honest caveats.
7. **Stop:** ask whether to push and open the PR.
8. **Open the PR** with `.github/PULL_REQUEST_TEMPLATE.md`. Include accepted deviations, departures from the plan and why, and what only CI can verify.
9. **Watch CI to the end.** Then give automated review about 5 minutes; if nothing arrives, report and stop. In a cloud session, subscribe to the PR's activity instead of polling.
10. **Handle review feedback:** verify each claim against the code, then fix it or reply why not, on its thread.
11. **Leave the branch and worktree** until the person chooses merge or discard.

## Commit gate

Run in this order before every commit, and never commit before the person's approval:

1. Build, `go vet`, and the tests of the touched packages; the full suite before the feature commit.
2. Lint exactly as `CLAUDE.md` says. A "0 issues" printed next to a type-check panic is a failure.
3. The race detector when the change touches concurrency.
4. A `reviewer` agent reviews the uncommitted diff (skip for comment-only commits). Fix what it finds and re-run 1 to 3; review once more only if the fixes were substantive, then report what is left instead of looping.
5. **Stop** for the person to review the diff.

## Commit discipline

- Order: comment audits, pure moves, interface reshaping, then the feature. A comment-only or pure-move commit holds nothing else.
- Audit a file's comments by `docs/standards/comments.md` before editing it, in its own commit. Prove it changed only comments: parse old and new with `go/parser` at mode 0, print with `go/printer` and compare. For a pure move, show every declaration and comment appears exactly once across the new files.
- Update `CLAUDE.md`, a rules file or a `doc.go` the change makes false. Never edit an accepted ADR; a changed decision gets a new one (the `add-adr` skill).
- A consistency question from the person ("why does X import Y?") is a design signal: answer from the code, then fix the shape in its own commit. Ask before filing an issue for deferred items.
- Stage explicit paths, never `git add .`.

## Subagents

A subagent that shares the session's checkout can commit on the wrong branch, overwrite another subagent's files, or stash and reset the work it reviews. The agents in `.claude/agents/` prevent that (`docs/arch/0040-isolated-subagents.md`), so never dispatch a general-purpose subagent for either job:
- **Code-writing work goes to `writer`.** It gets its own worktree and branch, started from the session's last commit, so commit first what it needs. Merge or cherry-pick the branch it reports.
- **Review goes to `reviewer`.** It can read files and run only `git diff`, `log`, `show`, `status`, `grep` and `ls-files`.

## Git safety

- To set work aside: `git stash push -u -m "<unique-tag>"`, note its SHA from `git stash list --format='%H %gs'`, restore with `git stash apply <sha>` (never `pop`; worktrees share the stash), and drop only your own entries by tag.
- If applying a stash conflicts with a later commit, take the committed file (`git checkout --ours <file>`), redo the small edits by hand, then `git reset` to a plain working tree.
