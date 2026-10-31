# Plan for #451: ADRs for the node pipeline

Status: **waiting for Tim's approval.** The ADR texts are the drafts in `docs/arch/0049-0054` on `claude/project-thread-wa6yxp` (not pushed). Nothing is posted to the ticket or written to `docs/arch/` until Tim approves.

Source: the approved design (https://claude.ai/artifact/EbpdXyA3RtYLSE2Pfc7Xfq) and the #443 decisions in the comment on #451.

## Decided with Tim (2026-10-31)

- One subject per ADR, no version or roadmap talk, no connective prose ("this amends X").
- Six ADRs ship in #451. The seventh, "a Jev or Kev judge's finding is a violation" (the ADR 0031 amendment), is written with #457, after its open details (dry-run, baseline, suggestion cache key, ADRs with no rules) are settled with Tim.
- When a diff is judged, a Jev or Kev judge compares before and after per rule.
- An `ask` node's `keep.top_k` ranks by the higher of its two sides' scores.
- A first-node `ask` defaults to `on_error: fail`, and `on_error: skip` on it is rejected.

- The six ADRs merge as `Proposed`; the PR that implements each flips it to `Accepted`, and #452 flips 0022 to `Superseded`.
- The LLM profile key naming is planned in #453. ADR 6's generic rule (a key goes only to the endpoint it's configured for) already covers a profile on another host.
- No brittle detail in the ADRs (Tim's review of the drafts): no runner, job, provider or env var names, no retired config keys, no file-level scopes. ADRs speak of nodes, the judging node, choosing nodes, providers and keys; names live in the README.

## Open questions

None.

## What changes

Six new ADRs, numbered from the next free number on `origin/main` (0049 today; re-checked before the PR). Nothing else:

- **ADR 0020 is left alone.** Its rules say `analysis.max_relevant_adrs` is the default `top_k` "of a configured `rank` stage"; a `rank` node is that stage, so nothing in it goes stale. This departs from the issue's AC, which asked to supersede its `top_k` rule.
- **ADR 0022 becomes `Superseded`** (status only). Its four Rules are restated in ADR 1. Its per-ADR `similarity_threshold` decision is already a rule of ADR 0011. Its "vector store returns an error, not an empty list" consequence isn't restated: ADR 0032 already fails a run left with nothing checked.
- **ADRs 0030 and 0031 are not edited.** 0030's Rules govern the LLM's diff and quote, which still hold. 0031's amendment moves to #457 with ADR 7.
- **`.claude/rules/config.md` is not touched;** #452 rewrites it.
- **The issue's AC is updated** to match: six ADRs, 0020 kept, 0031's amendment in #457, presets and LLM profiles documented in the README with their tickets (they are features, not decisions).

## The six ADRs (texts are the drafts)

1. `0049-pipeline-nodes.md`: the pipeline is made of nodes; only the judging node reports; scorers only score; suppressed ADRs removed first; an ADR a node has no question for passes on. Supersedes 0022.
2. `0050-nodes-run-in-order.md`: a list run top to bottom; the first node narrows; one judging node, only fix suggestions after it.
3. `0051-the-pipeline-is-always-written-out.md`: missing or invalid pipeline exits 3; retired shapes rejected, never aliased; validated by `check`, not at config load.
4. `0052-what-a-node-failure-does.md`: `on_error` skip/fail for choosing nodes; first node can't skip; skip passes ADRs on (a similarity ranker skips the file); fail picks the exit code by kind and outranks drift; judge/suggest failures take no `on_error`; a rejected key exits 3.
5. `0053-quoteless-runners-read-both-sides-of-a-diff.md`: before and after sides asked separately as plain text; missing side not asked; keep on either side, rank by the higher score; a judge reports per rule what passes after and not before.
6. `0054-provider-keys-stay-with-their-provider.md`: every key from the environment; a key goes only to the endpoint it's configured for.

## Verification

- `go test ./test/guidance`.
- Every `docs/arch/...` link in the new ADRs resolves.
- `/code-review xhigh` on the branch, then fixes, before the PR.
- Draft PR closing #451, ready when CI is green.

## Cleanup

`plan/451-node-adr` is listed in `/mnt/project-files/planning/throwaway-branches.md` and is deleted when #451's PR merges.
