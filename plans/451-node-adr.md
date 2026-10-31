# Plan for #451: ADR for the pipeline as a list of nodes

Source: the approved plan (https://claude.ai/artifact/EbpdXyA3RtYLSE2Pfc7Xfq) and the #443 decisions in the comment on #451.

## What changes

1. **New ADR `docs/arch/0049-the-pipeline-is-a-list-of-nodes.md`.** 0048 is the highest on main and no open PR claims 0049 (the dropped #439 ADR used 0049 on a branch only). Re-check right before the PR opens.
2. **`docs/arch/0020-configurable-topk-adr-limit.md` and `docs/arch/0022-candidate-scoring-pipeline.md`: `status` becomes `Superseded`.** Nothing else in them changes (the `add-adr` skill: only `status` changes in a superseded ADR).
3. **ADRs 0030 and 0031 are not edited.** They stay `Accepted`; the new ADR amends them with its own Rules. 0030's Rules are about quotes and about sending hunks, so they don't conflict. 0031 defines every finding whose quote isn't found as unverified, "including one with an empty quote", so the new ADR says plainly that a Jev or Kev finding is outside that definition; otherwise the drift check would flag #457 against 0031.

Nothing else. `.claude/rules/config.md` still points at ADR 0022 for `scorerNames`; #452 rewrites that file (as the #451 comment says), so it is left alone here.

## Why 0020 and 0022 become `Superseded`, not "partly superseded"

The AC asks that each superseded ADR's status says so. A status outside `analysis.accepted_statuses` makes the ADR silently ignored by the drift check, so a status such as "Partly superseded" would switch off all of 0022's rules, and adding a new accepted status would mean a config change for one ADR. Repo precedent (0043 to 0045, 0001 to 0044) is a plain `Superseded` with the new ADR carrying what still holds. So the new ADR restates the surviving Rules of 0020 and 0022 in node terms, and its Context names the 0022 decisions that carry over to `rank` and `ask` nodes: only scope-matched ADRs are candidates, a per-ADR `similarity_threshold` wins in every cosine node, a node error names the action and its kind, and `on_error: fail` outranks the drift exit code and stops `--update-baseline` writing. 0020's first rule (one per-file limit) is dropped on purpose, since an `ask` node's `keep.top_k` can now set its own.

## ADR outline

Frontmatter: `status: Accepted`. `scope`: `internal/analysis/**`, `internal/config/**`, `internal/cli/**`, `internal/inference/**`, `internal/output/**` (the packages the rules govern; output because 0031 lists violations there).

**Context** (short): the three fixed slots and what they can't do; this supersedes 0020's rank-stage `top_k` rule and 0022's pipeline-keys bullet and only-the-LLM-reports rule, with the rest of both carried over; amends 0030 and 0031; takes the decisions of #443 (ADR 0047, closed unmerged) and of the dropped Jev ranking ADR from #439.

**Decision** (bullets, decision-level only, no field-by-field config reference; that goes in the README with #452 to #458):

- `analysis.pipeline` is an ordered list of nodes. Each node has a `name`, a `job` and a runner in `with`: `rank` on cosine; `ask` on Jev or Kev; `judge` on an LLM, Jev or Kev; `suggest` on an LLM.
- Jev and Kev questions use Jev's types (`noul`, `choice`, `score`), asked per ADR or per rule, with your own `instructions` and `criteria`, and a `keep` (on `ask`) or `report` (on `judge`) rule. Two presets, `governs` and `breaks-a-rule`, name today's questions word for word, so the #202 thresholds still mean what they meant.
- Kev ships in v2 as a second runner behind the same client, so a pipeline can run fully offline.
- LLM profiles: named full `llm` blocks under `llms:`; an LLM node names one or uses `llm:`. ADR 0004's vendor boundary applies to each profile's key.
- The slot keys are removed with no aliases. `check` validates the pipeline when it builds it, so `archguard index` keeps working, and a missing or mapping-shaped pipeline exits 3 and says to run `archguard init`, which writes the default pipeline (cosine `rank`, LLM `judge`).
- Jev or Kev may judge: a finding has no quote or line, names the rule and probability, and counts as a violation (amends 0031).
- Under `--since`, Jev and Kev are asked about the before and after sides of the hunks; `ask` keeps an ADR that passes on either side; a Jev/Kev `judge` reports only what passes after and not before (amends 0030).
- The first node narrows the candidates: a `rank`, or an `ask` per ADR with a `keep.top_k`.
- A Jev or Kev `judge` that fails fails that file's checks like a failed LLM judge call, takes no `on_error`, and ADR 0032 applies.
- Errors, from #443: `ask` under `on_error: skip` passes its ADRs on unchanged (a first-node `ask` defaults to `fail`); a rejected key exits 3 whatever `on_error` says; credentials come only from environment variables.
- A chain for v2. Branching (a node naming its inputs) comes after v2 (#459) and keeps every chain config valid.

**Rules** (as committed, one MUST / MUST NOT each):

- Pipeline collaborators such as scorers, stages and nodes MUST be objects with methods, not bare functions.
- A scorer MUST only score its candidates; it MUST NOT drop or reorder them.
- A `rank` or `ask` node MUST NOT decide whether a file violates an ADR.
- An ADR suppressed by an `archguard-ignore: <ADR_ID>` comment MUST be removed before the first node runs.
- An unset or non-positive `analysis.max_relevant_adrs` MUST fall back to the default, so it MUST stay a plain `int`.
- A `rank` node's unset `threshold` and `top_k` MUST fall back to `vector_store.similarity_threshold` and `analysis.max_relevant_adrs`.
- A vector store whose backend fails MUST return an error, not an empty candidate list.
- An `analysis.pipeline` given as a mapping with `rank`, `rerank` or `pre_judge` keys MUST be rejected, not converted into nodes.
- A `check` whose `analysis.pipeline` is missing or not a list MUST exit 3.
- Pipeline validation MUST reject a first node that is neither a `rank` nor an `ask` with `for_each: adr` and a `keep.top_k`.
- Pipeline validation MUST reject a pipeline with no `judge` node, more than one, or any node after the `judge` other than one `suggest` node.
- Jev, Kev and LLM profile credentials MUST come from environment variables, never from `archguard.yaml`.
- A Kev request MUST NOT carry the Jev API key.
- An LLM profile whose provider differs from `llm.provider` MUST read `ARCHGUARD_<PROVIDER>_API_KEY` and MUST NOT fall back to `ARCHGUARD_API_KEY`.
- A Jev or Kev call rejected for its API key MUST fail the check with exit code 3, whatever `on_error` says.
- An `ask` node that is the first node MUST default to `on_error: fail`; any other `ask` MUST default to `skip`.
- An `ask` node that fails under `on_error: skip` MUST pass its ADRs on unchanged, unless its key was rejected or the run was cancelled.
- A node failure under `on_error: fail` MUST take precedence over the drift exit code, and `--update-baseline` MUST NOT write the baseline after one.
- An `ask` node with `for_each: rule` MUST pass on, without asking about it, an ADR that has no rules and no generated questions.
- When a file is judged by its diff, a Jev or Kev node MUST be asked about the before side and the after side of the hunks separately, and MUST NOT be sent the diff with its `+`/`-` markers.
- When a file is judged by its diff, an `ask` node MUST keep an ADR that passes on either side.
- When a file is judged by its diff, a Jev or Kev `judge` MUST report a rule, or with `for_each: adr` an ADR, only when it meets `report` after the change and not before.
- A Jev or Kev `judge` finding MUST count as a violation and MUST NOT be marked unverified.

**Alternatives considered**: more slots; aliasing the old keys; branching in v2; an LLM-only judge with Jev/Kev only as filters; "partly superseded" statuses (above).

**Consequences**: a Jev/Kev judge lists ADRs with no rules as "not judged: no rules" unless `generate_questions` covers them; breaking change for v2.0.0, users rerun `archguard init`; two Jev/Kev calls per node per file under `--since`; a Jev/Kev judge misses a change that worsens an existing violation, reports everything that breaks a rule in a new file, and nothing in a deleted file; a quote-less baseline entry suppresses that ADR for the whole file; until #452 lands, main's slot-based config code contradicts rules 7 and 9, so a PR touching it before #452 can be flagged by the drift check (no such PR is open; #452 is next and stacked on this).

## Decisions from Tim (2026-10-31)

- Under `--since`, a Jev or Kev judge compares before and after **per rule** (per ADR only with `for_each: adr`), so a newly broken rule is reported even when another rule of the same ADR was already broken.
- Under `--since`, an `ask` node's `keep.top_k` ranks each ADR by **the higher of its two sides' scores**.

## Review

Reviewed on `plan/451-node-adr` by a read-only agent. Fixed from it: rule 7 reworded so #452's "run archguard init" detection isn't flagged; #443's default, exceptions and rule-less pass-through added (rules 13, 14); 0031's empty-quote wording handled; first-node narrowing and the failed Jev/Kev judge added to the Decision; carried-over 0022 points named; `internal/output/**` added to scope; rule 9 rewritten.

## Code review fixes (/code-review xhigh)

13 findings fixed without changing the approved design: the before/after split covers every diff that's judged (ADR 0037 too) and never sends `+`/`-` markers; a missing side isn't asked; Kev never receives Jev's key; rules for the first node, `on_error` defaults, `on_error: fail` precedence and the vector store error rule from 0022; ADR 0016 still gates `suggest`; `generate_questions` defined; the slot-key rule reworded so a `rank` node isn't read as the old `rank` key; `init` described as adding the pipeline without touching other keys; Consequences say every existing config breaks, not only slot configs; `internal/index/**` added to scope. Two gaps went to Tim (above).

## Verification

- `go test ./test/guidance` (template headings and scope).
- `go test ./...` to confirm nothing reads ADR 0020/0022 status in a test fixture.
- Index the repo locally if a provider is reachable, so the new ADR's rules parse; otherwise the CI drift check is the first real parse, and the PR says so.
- `/code-review xhigh` on the branch before the PR.

## PR

One commit, `docs: decide that the pipeline is a list of nodes`, on `claude/project-thread-wa6yxp`; draft PR closing #451, marked ready when CI is green. #452 stacks on it.
