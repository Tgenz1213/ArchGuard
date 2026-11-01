---
title: Runners that answer without a quote read both sides of a diff
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/inference/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Runners that answer without a quote read both sides of a diff

## Context

When a file is judged by its diff, an LLM's quote shows whether a finding sits in added, removed or unchanged lines. Some runners answer questions with a probability and no quote, so that check can't work for them, and a change can break an ADR by removing code it requires.

## Decision

- **A runner that answers questions without a quote is asked about each side of the diff separately, as plain text.** The before side is the unchanged and removed lines; the after side is the unchanged and added lines. A new file has no before side and a deleted file no after side; a side that doesn't exist isn't asked, and counts as nothing found.
- **A choosing node keeps an ADR it would keep on either side,** ranked by the higher of its two scores, so a filter never drops what a removal might break.
- **A judge reports what the change introduced:** a rule it finds broken after the change and not before, or the ADR when it asks per ADR.

## Rules

- When a file is judged by its diff, a runner that answers questions without a quote MUST be asked about the before side and the after side separately, and MUST NOT be sent the diff's `+`/`-` markers.
- A side of a diff that doesn't exist MUST NOT be sent.
- When a file is judged by its diff, a choosing node that asks such a runner MUST keep an ADR it would keep on either side, and MUST rank it by the higher of its two scores.
- When a file is judged by its diff, a judge that answers without a quote MUST report a rule, or an ADR it asks about as a whole, only when it finds it broken after the change and not before.

## Alternatives considered

- **Send the diff with its markers.** A removed guard reads as present code, so removing it looks compliant.
- **Ask about the after side only.** Removing code an ADR requires leaves nothing to find.
- **Compare per ADR in a judge.** A newly broken rule goes unreported when another rule of the same ADR was already broken.

## Consequences

- Two calls per node per file when a diff is judged.
- A change that makes an already broken rule worse isn't reported by such a judge.
- A new file is judged on its after side alone, so anything in it that breaks a rule is reported.
- A rule that depends on code outside the hunks can be missed.
