# Plan for #451: ADR for the pipeline as a list of nodes

Source: the approved plan (https://claude.ai/artifact/EbpdXyA3RtYLSE2Pfc7Xfq) and the #443 decisions in the comment on #451.

## What changes

1. **New ADR `docs/arch/0049-the-pipeline-is-a-list-of-nodes.md`.** 0048 is the highest on main and no open PR claims 0049 (the dropped #439 ADR used 0049 on a branch only). Re-check right before the PR opens.
2. **`docs/arch/0020-configurable-topk-adr-limit.md` and `docs/arch/0022-candidate-scoring-pipeline.md`: `status` becomes `Superseded`.** Nothing else in them changes (the `add-adr` skill: only `status` changes in a superseded ADR).
3. **ADRs 0030 and 0031 are not edited.** They stay `Accepted`; the new ADR amends them with its own Rules. Neither of their Rules conflicts with the amendment: 0030's quote rule and 0031's unverified rules only apply to findings that have a quote.

Nothing else. `.claude/rules/config.md` still points at ADR 0022 for `scorerNames`; #452 rewrites that file (as the #451 comment says), so it is left alone here.

## Why 0020 and 0022 become `Superseded`, not "partly superseded"

The AC asks that each superseded ADR's status says so. A status outside `analysis.accepted_statuses` makes the ADR silently ignored by the drift check, so a status such as "Partly superseded" would switch off all of 0022's rules, and adding a new accepted status would mean a config change for one ADR. Repo precedent (0043 to 0045, 0001 to 0044) is a plain `Superseded` with the new ADR carrying what still holds. So the new ADR restates the rules of 0020 and 0022 that survive, in node terms, and its Context says everything else those two decided carries over to `rank` and `ask` nodes.

## ADR outline

Frontmatter: `status: Accepted`. `scope`: `internal/analysis/**`, `internal/config/**`, `internal/cli/**`, `internal/inference/**` (the packages the rules govern; 0022 and 0020 used `internal/**`, narrowed per the `add-adr` skill).

**Context** (short): the three fixed slots and what they can't do; this supersedes 0020's rank-stage `top_k` rule and 0022's pipeline-keys bullet and only-the-LLM-reports rule, with the rest of both carried over; amends 0030 and 0031; takes the decisions of #443 (ADR 0047, closed unmerged) and of the dropped Jev ranking ADR from #439.

**Decision** (bullets, decision-level only, no field-by-field config reference; that goes in the README with #452 to #458):

- `analysis.pipeline` is an ordered list of nodes. Each node has a `name`, a `job` and a runner in `with`: `rank` on cosine; `ask` on Jev or Kev; `judge` on an LLM, Jev or Kev; `suggest` on an LLM.
- Jev and Kev questions use Jev's types (`noul`, `choice`, `score`), asked per ADR or per rule, with your own `instructions` and `criteria`, and a `keep` (on `ask`) or `report` (on `judge`) rule. Two presets, `governs` and `breaks-a-rule`, name today's questions word for word, so the #202 thresholds still mean what they meant.
- Kev ships in v2 as a second runner behind the same client, so a pipeline can run fully offline.
- LLM profiles: named full `llm` blocks under `llms:`; an LLM node names one or uses `llm:`.
- The slot keys are removed with no aliases. `check` validates the pipeline when it builds it, so `archguard index` keeps working, and a missing or mapping-shaped pipeline exits 3 and says to run `archguard init`, which writes the default pipeline (cosine `rank`, LLM `judge`).
- Jev or Kev may judge: a finding has no quote or line, names the rule and probability, and counts as a violation (amends 0031).
- Under `--since`, Jev and Kev are asked about the before and after sides of the hunks; `ask` keeps an ADR that passes on either side; a Jev/Kev `judge` reports only what passes after and not before (amends 0030).
- Errors, from #443: `ask` under `on_error: skip` passes its ADRs on unchanged (a first-node `ask` defaults to `fail`); a rejected key exits 3 whatever `on_error` says; credentials come only from environment variables.
- A chain for v2. Branching (a node naming its inputs) comes after v2 (#459) and keeps every chain config valid.

**Rules** (one MUST / MUST NOT each, checkable against one file):

Carried over from 0022 and 0020:
1. Pipeline collaborators such as scorers, stages and nodes MUST be objects with methods, not bare functions.
2. A scorer MUST only score its candidates; it MUST NOT drop or reorder them.
3. A `rank` or `ask` node MUST NOT decide whether a file violates an ADR; only the `judge` node reports violations.
4. An ADR suppressed by an `archguard-ignore: <ADR_ID>` comment MUST be removed before the first node runs.
5. An unset or non-positive `analysis.max_relevant_adrs` MUST fall back to the default, so it MUST stay a plain `int`.
6. A `rank` node's unset `threshold` and `top_k` MUST fall back to `vector_store.similarity_threshold` and `analysis.max_relevant_adrs`.

New:
7. `analysis.pipeline` MUST be read as an ordered list of nodes, and the `rank`, `rerank` and `pre_judge` slot keys MUST NOT be read or aliased.
8. Each node MUST receive only the ADRs the node above it kept.
9. A pipeline MUST have exactly one `judge` node, and only a single `suggest` node MAY follow it. (Written as MUST NOT: "No node other than one `suggest` node MUST come after the `judge` node.")
10. Jev, Kev and LLM profile credentials MUST come from environment variables, never from `archguard.yaml`.
11. An LLM profile whose provider differs from `llm.provider` MUST read `ARCHGUARD_<PROVIDER>_API_KEY` and MUST NOT fall back to `ARCHGUARD_API_KEY`.
12. A Jev or Kev call rejected for its API key MUST fail the check with exit code 3, whatever `on_error` says.
13. An `ask` node that fails under `on_error: skip` MUST pass its ADRs on unchanged; it MUST NOT drop them or skip the file.
14. Under `--since` without `--whole-file`, a Jev or Kev node MUST be asked about the before side and the after side of the hunks separately.
15. Under `--since`, an `ask` node MUST keep an ADR that passes on either side, and a Jev or Kev `judge` MUST report an ADR only when it meets `report` after the change and not before.
16. A Jev or Kev `judge` finding MUST count as a violation and MUST NOT be marked unverified.
17. A Jev or Kev `judge` MUST list an ADR it could not judge for lack of rules as not judged; it MUST NOT drop it silently.

**Alternatives considered**: more slots; aliasing the old keys; branching in v2; an LLM-only judge with Jev/Kev only as filters; "partly superseded" statuses (above).

**Consequences**: breaking change for v2.0.0, users rerun `archguard init`; two Jev/Kev calls per node per file under `--since`; a Jev/Kev judge misses a change that worsens an existing violation, reports everything that breaks a rule in a new file, and nothing in a deleted file; a quote-less baseline entry suppresses that ADR for the whole file; until #452 lands, main's slot-based config code contradicts rules 7 to 9, so a PR touching it before #452 can be flagged by the drift check (no such PR is open; #452 is next and stacked on this).

## Verification

- `go test ./test/guidance` (template headings and scope).
- `go test ./...` to confirm nothing reads ADR 0020/0022 status in a test fixture.
- Index the repo locally if a provider is reachable, so the new ADR's rules parse; otherwise the CI drift check is the first real parse, and the PR says so.
- `/code-review xhigh` on the branch before the PR.

## PR

One commit, `docs: decide that the pipeline is a list of nodes`, on `claude/project-thread-wa6yxp`; draft PR closing #451, marked ready when CI is green. #452 stacks on it.
