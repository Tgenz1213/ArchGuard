# Review schema

The shapes that pass between the steps of `pipeline.md`. Reviewers return findings, triage turns findings into claims, verifiers return verdicts.

## Finding

A reviewer returns a JSON array of findings, or `none`. Each finding is one object:

| Field | Value |
|---|---|
| `role` | The reviewer role's `name`. |
| `kind` | `break` or `design`. |
| `file` | Repository path of the code. A missing-scope finding names the ticket (`#<N>`). |
| `line` | Line number in the new version, or `null` for a missing-scope finding. |
| `claim` | One sentence: what is wrong. |
| `evidence` | The code or diff read, quoted. |
| `trigger` | `break` only: the input or path that fails. |
| `basis` | `design` only: the quoted rule, the named replacement, the duplicated location or the unmet acceptance criterion. |
| `repro` | Optional: the command that shows it, with any throwaway test's source and the output seen. |
| `severity` | `blocker`, `major` or `minor`. |
| `confidence` | 0 to 1. |

```json
{"role": "breaker", "kind": "break", "file": "internal/git/diff.go", "line": 42, "claim": "...", "evidence": "...", "trigger": "...", "repro": "...", "severity": "major", "confidence": 0.8}
```

A finding without its kind's evidence (`file`, `evidence` and `trigger` for `break`; `evidence` and `basis` for `design`, and `line` where code exists) is dropped by triage. A `break` finding with no `line` is dropped too.

## Claim

Triage turns each surviving finding into a claim and strips `role`, `severity` and `confidence`, along with the reviewer's reasoning:

| Field | Value |
|---|---|
| `id` | `C1`, `C2`, ... in triage rank order. |
| `kind` | As in the finding. |
| `file`, `line` | As in the finding. |
| `claim` | Neutral wording: what is wrong, where, and the trigger (`break`) or the basis (`design`). It carries them, so a claim has no separate `trigger` or `basis` field. |
| `evidence` | As in the finding. |
| `repro` | As in the finding, if present. |

Triage also returns each finding it dropped as `{finding, reason}`, with the finding unchanged.

## Verdict

A verifier returns one object in the YAML its role file shows:

| Field | Value |
|---|---|
| `verdict` | `confirmed`, `refuted` or `unresolved`. |
| `evidence` | Commands, output, searches or `file:line` citations that decide it. |
| `severity` | Optional: `blocker`, `major` or `minor`. Impact always returns one. |
| `introduced` | Provenance only: `yes`, `no` or `unclear`. |
| `scope` | Provenance only: `in`, `out` or `unknown` (no ticket). |
| `actionable` | Impact only: `yes` or `no`. |

What `confirmed` means depends on the role:

| Role | `confirmed` | `refuted` | `unresolved` |
|---|---|---|---|
| reproducer | The failure was shown by running it. | The trigger gave the correct result. | Could not run, or ambiguous output. |
| guard-hunter | No guard, helper or exception found that makes the claim wrong. | A guard, helper or exception, cited. | Could not tell. |
| provenance | `introduced: yes` | `introduced: no` | `introduced: unclear` |
| impact | `actionable: yes` | Never returned. | `actionable: no` |

A `refuted` verdict without cited evidence counts as `unresolved`. A verifier that could not run counts as `unresolved`.

## Report labels

The report and any posted comments use Conventional Comments labels:

| Finding | Label |
|---|---|
| Confirmed `break`, severity `blocker` or `major` | `issue (blocking):` |
| Confirmed `break`, severity `minor` | `issue (non-blocking):` |
| Confirmed `design`, severity `blocker` or `major` | `suggestion (blocking):` |
| Confirmed `design`, severity `minor` | `suggestion (non-blocking):` |
| Confirmed with `actionable: no` | `suggestion (non-blocking):` |
| Suspected | `question:` |

A confirmed finding with `actionable: no` takes the `actionable: no` row whatever its severity.
