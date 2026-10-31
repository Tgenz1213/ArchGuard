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

## The six ADRs

Each is short: Context of one or two sentences, a few Decision bullets, Rules, Alternatives, Consequences. Scopes are kept narrow so the repo's own drift check, which keeps the top 6 ADRs per file, isn't crowded by four pipeline ADRs on every analysis file.

### 1. The pipeline is made of nodes

Scope: `internal/analysis/**`, `internal/config/**`. Supersedes 0022.

Decision: a node has a `name`, a `job` and a runner (`with`). `rank` runs on cosine and keeps by `threshold`/`top_k`. `ask` runs on Jev or Kev, asks one question per ADR or per rule (`for_each`), and keeps by its `keep` setting. `judge` runs on an LLM, Jev or Kev and is the only node that reports violations; a Jev or Kev judge reports by its `report` setting. `suggest` runs on an LLM. A scorer scores; its node keeps or drops.

Rules:
- Pipeline collaborators such as scorers and nodes MUST be objects with methods, not bare functions.
- A scorer MUST only score its candidates; it MUST NOT drop or reorder them.
- A `rank` or `ask` node MUST NOT decide whether a file violates an ADR.
- An ADR suppressed by an `archguard-ignore: <ADR_ID>` comment MUST be removed before the first node runs.
- An `ask` node MUST pass on, without asking, an ADR it has no question for (a per-rule `ask` and an ADR with no rules).

Alternatives: more slots; Jev and Kev only as filters.

### 2. Nodes run in list order

Scope: `internal/config/**`, `internal/analysis/file_check.go`. The one ADR a branching graph would supersede.

Decision: `analysis.pipeline` is a list run top to bottom, each node on what the node above kept. The first node is a `rank`, or an `ask` per ADR with a `keep.top_k`. Exactly one `judge`; only one `suggest` may follow it.

Rules: validation MUST reject a first node that is neither of those; validation MUST reject no `judge`, more than one, or any node after it other than one `suggest`.

Alternatives: a graph where nodes name their inputs (routing, merging disagreeing judges, per-path reporting).

### 3. The pipeline is always written out

Scope: `internal/config/**`, `internal/cli/**`.

Decision: `check` needs a list-shaped `analysis.pipeline`. A missing one, the old `rank`/`rerank`/`pre_judge` mapping, or any validation failure exits 3 and names the node or key. No aliases. The pipeline is validated when `check` builds it, not when the config loads, so `archguard index` works on a config only `check` rejects. (What the error message shows and how `init` helps are README behaviour for #452, not in the ADR.)

Rules: the old mapping MUST be rejected, not converted; a `check` whose pipeline is missing, not a list or invalid MUST exit 3; loading the config MUST NOT validate the pipeline.

Alternatives: aliases; a missing pipeline meaning the default.

### 4. What a node failure does

Scope: `internal/analysis/**`, `internal/cli/**`.

Decision: `on_error` (`skip` or `fail`) applies to `rank` and `ask` nodes only. Default `skip`, except a first-node `ask`, which defaults to `fail` and rejects `skip`. Under `skip`, a failed `rank` skips the file and a failed `ask` passes its ADRs on unchanged. Under `fail`, the failure is recorded, that file's later nodes stop, other files still run, and the failure's kind (a dependency that didn't respond, or a precondition not met) picks the exit code, which outranks drift; `--update-baseline` then writes nothing. A failed `judge` or `suggest` call works as an LLM judge or suggestion call does today and takes no `on_error`. A rejected Jev or Kev key exits 3 whatever `on_error` says.

Rules: one per point above, each a single MUST.

Alternatives: pass on everything on any failure; fail on every failure.

### 5. Jev and Kev read both sides of a diff

Scope: `internal/analysis/stage/**`, `internal/inference/**`.

Decision: when a file is judged by its diff, Jev and Kev get the before side (unchanged and removed lines) and the after side (unchanged and added lines) separately, as plain text, never the `+`/`-` diff. A side that doesn't exist isn't sent. An `ask` keeps an ADR that passes on either side and ranks it by the higher score. A Jev or Kev judge reports a rule (or the ADR, per ADR) that meets `report` after and not before.

Rules: one per point above.

Alternatives: send the marked diff (a removed guard reads as present code); after side only (a removal leaves nothing to find); compare per ADR (misses a newly broken second rule).

Consequences: two calls per node per file on a diff; a worsened, already broken rule isn't reported by a Jev or Kev judge; a rule that depends on code outside the hunks can be missed.

### 6. Runner keys come from the environment and go only to their runner

Scope: `internal/config/**`, `internal/cli/**`, `internal/inference/**`.

Decision: Jev reads `ARCHGUARD_JEV_API_KEY` and Kev `ARCHGUARD_KEV_API_KEY`; keys never come from `archguard.yaml`; a key is sent only to its own runner's endpoint; a key is required only when a node uses its runner, checked when `check` builds the pipeline. LLM profile keys: depends on Q4.

Rules: keys MUST come from environment variables; a runner's key MUST NOT be sent to any other runner's endpoint.

## Status (Q3)

On main, the slot config and the `pre_judge` stage contradict ADRs 2, 3 and 5 until #452 lands, and the drift check reads `Accepted` ADRs only. Recommended: the six ADRs merge as `Proposed`, and the PR that implements each one flips it to `Accepted` (and #452 flips 0022 to `Superseded`). Then no PR in between is flagged for code that hasn't changed yet, and 0022 keeps guarding the stage code until it's replaced.

## Verification

- `go test ./test/guidance`.
- Every `docs/arch/...` link in the new ADRs resolves.
- `/code-review xhigh` on the branch, then fixes, before the PR.
- Draft PR closing #451, ready when CI is green.

## Cleanup

`plan/451-node-adr` is listed in `/mnt/project-files/planning/throwaway-branches.md` and is deleted when #451's PR merges.
