---
title: Unverified findings are reported but do not fail the run
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/cli/**"
  - "internal/output/**"
---

# Unverified findings are reported but do not fail the run

## Context

The engine checks every finding's quoted code against what the model read. A finding whose quote it could not find still counted as a violation and made the run exit `4`. Two kinds of model error fail a run this way: on #277, under `--since`, the model quoted an unchanged line against a broad rule, and in another run it quoted a sentence from the ADR instead of the file (#229). A finding that points at no code in the file cannot be checked or fixed, and failing a pull request on it makes the gate noisy. See #292 and #229.

## Decision

- **A finding is verified when its quote is found in what the model read.** Under `--since`, that is an added or removed line (`docs/arch/0030-judge-the-change-under-since.md`). Every other finding is unverified, including one with an empty quote.
- **An unverified finding is not a violation.** It does not count toward the violation count or the drift exit code `4`, and gets no suggested fix. A run whose only findings are unverified passes.
- **An unverified finding is still reported, apart from violations,** in the text report, the JSON document and the log. In GitHub Actions it is a warning, not an error.
- **Baselining is unchanged** (`docs/arch/0006-violation-baseline-file.md`). A baselined `(ADR, file)` pair hides an unverified finding as it hides a verified one.

## Rules

- Code that counts an unverified finding toward `count` or the drift exit code `4`.
- Code that lists an unverified finding among `violations` in the text report or the JSON document.

## Alternatives considered

- **Keep unverified findings as violations.** Simple, but a model's mistake fails the pull request, which is what #277 did.
- **Warn in CI and fail locally, or the reverse.** The same change would pass in one place and fail in the other.
- **A separate exit code for a run with only unverified findings.** Most pipelines treat every non-zero code as a failure, so it would fail the same runs it was meant to pass.
- **Ask the model again for a verbatim quote.** One more LLM call per bad quote, and unwrapping markdown around a quote already fixes the most common cause. The evaluation set (#288) can show whether it is worth it.
- **Drop unverified findings.** A model that paraphrases a real violation would hide it entirely.

## Consequences

- A model that paraphrases a real violation does not fail the run. The finding stays in the report for a person to read, so how often a model paraphrases decides how much the gate catches.
- `--format json` readers that looked for unverified findings in `violations` must read `unverified`. This is a breaking change for v2.0.0.
- A new way to check a finding, such as verifying the rule the model cites (#290), marks a failed check as unverified and gets this handling.
