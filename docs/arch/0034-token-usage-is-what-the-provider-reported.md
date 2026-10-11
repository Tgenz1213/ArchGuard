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

- **Figures are what each provider reported for each call.** `Chatter.Chat` returns a `ChatReply` holding the text and its `Usage`, and `Embedder.CreateEmbedding` returns an `Embedding` holding the vector and its `Usage`. Each provider fills `Usage` from its own response (Claude's and OpenAI's `usage`, Gemini's `usageMetadata` with thinking tokens counted as output, Ollama's final `prompt_eval_count` and `eval_count`, OpenAI's and Voyage's embedding `usage`). Nothing is recounted.
- **A figure the provider did not report is unavailable, never zero.** `Usage.Reported` is false when the response carried no usage, which is the zero value, so a provider that forgets to set it shows unavailable rather than a wrong zero. A total is unavailable when any call in it was unreported, because a partial sum would undercount. Gemini's and Ollama's embedding endpoints report nothing, so their embedding figures are always unavailable.
- **Usage is totalled at the provider boundary, one meter per kind of call.** `cli` wraps the chat provider in two `telemetry.Chatter`s, one handed to `Engine.Chat` for judgments and one to `Engine.SuggestChat` for suggestions, and the embedder in one `telemetry.Embedder` that serves both the index rebuild and the scoring stages. A cache hit makes no call, so it adds nothing. A retried call adds each attempt that returned, since each was billed. A call that failed adds nothing, since there is no usage to read. `CountTokens`, which only sizes a large file, is not a chat call and is not counted.
- **Cost is priced from config, with no built-in prices.** `llm.cost_per_1k_input_tokens`, `llm.cost_per_1k_output_tokens` and `vector_store.cost_per_1k_tokens` are optional. When set, the report multiplies each available figure by its price; unset, it shows tokens only. The two chat prices are set together or not at all, and a negative price is a config error. The unit is whatever currency the prices are in.
- **`--dry-run` counts the judgments instead of making them.** Everything up to the judgment calls runs for real, embeddings and any index rebuild included, and those embeddings are reported as usual. Each judgment that a cached verdict would not answer is counted with `CountTokens` over its system and user prompt, and nothing is sent or cached. The output tokens are not projected, because they depend on the answer, so the projected cost prices the input alone. Suggestions follow violations, which a dry run never finds, so their cost is not projected and the run says so in one line. A dry run reports no violations, lists what it could not count as failed ADR checks, and exits `0` unless an error stopped the run itself. It cannot be combined with `--update-baseline`.
- **The report shows usage in both formats.** The text report ends with one `Tokens:` line. The `--format json` document carries a `usage` object with `judgment`, `embedding` and, under `--suggest-fixes`, `suggestion`, where an unavailable count or cost is `null`.

## Rules

- A provider's `Chat` or `CreateEmbedding` that sets `Usage.Reported` to true without a token count from the backend's response.
- A table of model prices built into the binary.
- A `--dry-run` path that sends a judgment or suggestion chat call, or writes to the analysis cache.

## Alternatives considered

- **Recount every prompt with `CountTokens`.** It costs an extra round trip per call on Ollama and Gemini, and it can differ from what the provider billed.
- **A built-in price table.** Prices change and vary by account and region, so the binary would go stale and be wrong without saying so.
- **Counters inside each provider.** One provider instance serves judgments and suggestions concurrently, so it cannot tell them apart; a wrapper per kind of call can.
- **Passing a recorder through the context.** It hides a dependency that the rest of the engine passes explicitly. A recorder seam injected like `Chat` and `Embed` is #262.
- **Showing an unreported figure as `0`.** It reads as free.
- **Projecting dry-run output tokens from a fixed guess per call.** It would be a number nobody measured, shown next to ones that were.

## Consequences

- A new provider fills `Usage` from its response, or leaves it unreported. Tests cover both cases for every provider.
- The figures are an estimate of the bill, not the bill: provider discounts, cached-input pricing, failed calls that were still billed and price changes are not reflected.
- Changing `Chat` and `CreateEmbedding` to return structs touched every caller and test double; `MockProvider` keeps returning plain strings and vectors from its funcs and attaches `ChatUsage` and `EmbedUsage`.
