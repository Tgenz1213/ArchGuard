---
title: Token usage is what the provider reported, and cost is priced from config
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/inference/**"
  - "internal/cli/**"
  - "internal/output/**"
---

# Token usage is what the provider reported, and cost is priced from config

## Context

`check` calls a paid API for every provider except Ollama: one chat call per file and relevant ADR, one more per violation under `--suggest-fixes`, and embedding calls for each file's query and any index rebuild. None of it was measured, so a large `--all` run could cost real money with nothing to show for it. Measuring it raises three questions: where the numbers come from, how a provider that reports nothing is shown, and where prices come from. See #72.

## Decision

- **Figures are what each provider reported for each call.** Each chat and embedding call returns the usage from the provider's own response. Nothing is recounted.
- **A figure the provider did not report is unavailable, never zero.** Unreported is the zero value, so a provider that forgets to fill it shows unavailable rather than a wrong zero. A total is unavailable when any call in it was unreported, because a partial sum would undercount.
- **Usage is totalled at the provider boundary, one meter per kind of call:** judgments, suggestions and embeddings. Only calls that returned are counted, so a cache hit or a failed call adds nothing and each returned retry adds its own. Sizing a large file is not a chat call and is not counted.
- **Cost is priced from config, with no built-in prices.** The prices are optional; unset, the report shows tokens only. The unit is whatever currency the prices are in.
- **`--dry-run` counts the judgments instead of making them.** Everything before the judgment calls runs for real, embeddings included. Each judgment a cached verdict would not answer is counted from its prompt, and nothing is sent or cached. Output tokens and suggestions depend on answers a dry run never gets, so they are not projected. A dry run reports no violations and cannot be combined with `--update-baseline`.
- **The report shows usage in both formats,** with an unavailable figure shown as unavailable.

## Rules

- A provider's `Chat` or `CreateEmbedding` MUST NOT set `Usage.Reported` to true without a token count from the backend's response.
- Model prices MUST NOT be built into the binary.
- A `--dry-run` path MUST NOT send a judgment or suggestion chat call, or write to the analysis cache.

## Alternatives considered

- **Recount every prompt with `CountTokens`.** It costs an extra round trip per call on Ollama and Gemini, and it can differ from what the provider billed.
- **A built-in price table.** Prices change and vary by account and region, so the binary would go stale and be wrong without saying so.
- **Counters inside each provider.** One provider instance serves judgments and suggestions concurrently, so it cannot tell them apart; a wrapper per kind of call can.
- **Passing a recorder through the context.** It hides a dependency that the rest of the engine passes explicitly. A recorder seam injected like `Chat` and `Embed` is #262.
- **Showing an unreported figure as `0`.** It reads as free.
- **Projecting dry-run output tokens from a fixed guess per call.** It would be a number nobody measured, shown next to ones that were.

## Consequences

- A new provider fills usage from its response, or leaves it unreported. Tests cover both cases for every provider.
- The figures are an estimate of the bill, not the bill: provider discounts, cached-input pricing, failed calls that were still billed and price changes are not reflected.
- Some embedding endpoints report no usage, so their figures are always unavailable.
- Chat and embedding calls return usage alongside their result, which touched every caller and test double.
