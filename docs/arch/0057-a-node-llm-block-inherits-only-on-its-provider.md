---
title: A node's llm block inherits from llm only on llm's provider
status: Archived
scope:
  - "internal/config/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# A node's llm block inherits from llm only on llm's provider

## Context

A `judge` or `suggest` node can take its own `llm` block and leave out the keys it shares with the top-level `llm` block. Most of those keys are only meaningful to one provider: a model name, a `base_url`, a token limit, a temperature range and prices. Inherited across a provider switch, they send another provider's model name, point at the wrong host or price the run wrongly, with no error.

## Decision

- **On `llm.provider`, or with no `provider`, a node's block inherits every key it leaves out from `llm`.**
- **On another provider, the block starts fresh.** It inherits only `system_prompt`, and only on the judge, since a `suggest` node can't set one. It must set `model`, and every other key it leaves out takes that provider's default.
- **Prices come as a pair.** A node that sets one of `cost_per_1k_input_tokens` and `cost_per_1k_output_tokens` sets both, and never inherits the other half.

## Rules

- A node's `llm` block that sets a provider other than `llm.provider` MUST NOT inherit any key from `llm` except the judge's `system_prompt`.
- A node's `llm` block that sets a provider other than `llm.provider` without a `model` MUST be rejected at startup.
- A node's `llm` block that sets only one of the two chat prices MUST be rejected at startup.

## Alternatives considered

- **Drop only the model, `base_url` and prices on a switch.** A temperature or token limit tuned for one provider still carries over to another.
- **Inherit everything, but require `model` on a switch.** `base_url` and prices still carry over, and a wrong price is never reported.
- **Inherit the other price.** A node's cost would mix one provider's input price with another's output price.

## Consequences

- A node on another provider repeats the few keys it needs, such as `max_tokens` and `temperature`, instead of inheriting `llm`'s silently.
- A node can't change only one price.
