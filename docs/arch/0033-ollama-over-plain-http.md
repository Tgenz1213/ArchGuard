---
title: Talk to Ollama over plain HTTP, not the ollama Go module
status: Accepted
scope:
  - "internal/inference/**"
  - "go.mod"
---

# Talk to Ollama over plain HTTP, not the ollama Go module

## Context

`OllamaProvider` used the `api` client package of `github.com/ollama/ollama`. That module is the Ollama server, and the Go vulnerability database lists advisories against the whole module (GO-2025-3557, GO-2025-3558, GO-2025-3559, GO-2025-3582, GO-2025-3689, GO-2025-3695, GO-2025-3824, GO-2025-4251) with no fixed version. The advisories describe the server, but because they cover every package, `govulncheck` reports them as reachable through the `api` client, and no upgrade clears them. CI fails on reachable vulnerabilities, and the project does not exempt findings.

ArchGuard calls three endpoints: `/api/chat`, `/api/embeddings` and `/api/generate`, each with a single JSON response (`stream: false`).

## Decision

- **`OllamaProvider` calls Ollama's REST API with `net/http`,** the same way `VoyageProvider` calls Voyage. The request and response fields are the ones the endpoints document, and `ollama_test.go` pins them with `httptest`.
- **A failure becomes `ollama api error: ...`,** using the `error` field of Ollama's JSON body when there is one, including on a 2xx response.
- **`go.mod` does not require `github.com/ollama/ollama`.**

## Rules

- Code that imports `github.com/ollama/ollama` or any of its packages.

## Alternatives considered

- **Exempting the advisories in `govulncheck`.** Rejected: the project fixes findings rather than exempting them.
- **Ollama's OpenAI-compatible `/v1` endpoints through `OpenAIProvider`.** Rejected: they have no equivalent of `/api/generate`'s `prompt_eval_count`, which `CountTokens` needs (`docs/arch/0002-provider-scoped-token-counting.md`).
- **A community Ollama client library.** Rejected: a less-vetted dependency for three JSON calls.

## Consequences

- An Ollama API change that the official client would have absorbed has to be made here. The three endpoints have been stable, and the e2e and unit tests cover the fields ArchGuard sends and reads.
- One dependency tree (the Ollama server's) leaves `go.sum`.
- The module's client signed requests with the local `~/.ollama` key when `base_url` was `ollama.com` or `OLLAMA_AUTH` was set. ArchGuard never documented either, and now sends unsigned requests, so Ollama's hosted API is not supported.
