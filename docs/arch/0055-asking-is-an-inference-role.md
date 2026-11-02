---
title: Asking is an inference role beside chat and embedding
status: Accepted
scope:
  - "internal/inference/**"
  - "internal/analysis/**"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Asking is an inference role beside chat and embedding

## Context

Jev and Kev don't chat or embed. They take a text and named questions and return a probability for each answer. The pipeline's nodes need to know when such a model refused its key or got an input that was too large, because those failures stop a check or change its exit code, but a node shouldn't depend on which model answered.

## Decision

- **Asking is a third model role, `inference.Asker`, beside `inference.Chatter` and `inference.Embedder`.** Code that asks questions takes an `Asker`, the way code that embeds takes an `Embedder`.
- **An `Asker` reports a refused key and an oversized input as errors that match `inference.ErrKeyRejected` and `inference.ErrInputTooLarge` with `errors.Is`.** Code outside the provider classifies an asking failure only through those, never through a provider's own error type.

## Rules

- Code that asks a model questions MUST take an `inference.Asker`, not a provider type.
- An `inference.Asker` MUST return errors that match `inference.ErrKeyRejected` when its key is refused and `inference.ErrInputTooLarge` when its input is too large.
- Code outside `internal/inference` MUST NOT inspect a provider's own error type to classify an asking failure.

## Alternatives considered

- **Keep the interface in the node package, where it is consumed.** The node then had to read Jev's error type to classify failures, so a second asker would have needed node changes.
- **One error type shared by every asker.** Each provider would have to translate its responses into a struct it doesn't own, where a sentinel needs only an `Is` method.

## Consequences

- A new asker, such as a Kev client, works in an ask node without changes to the node.
- A new asker that doesn't map its key and size errors onto the sentinels has its revoked key treated as an outage, so the provider's tests must cover both mappings.
