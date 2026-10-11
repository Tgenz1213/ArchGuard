---
title: Talk to Ollama over plain HTTP, not the ollama Go module
status: Accepted
scope:
  - "internal/inference/**"
  - "go.mod"
---

# Talk to Ollama over plain HTTP, not the ollama Go module

## Context

The Ollama provider used the `api` client package of `github.com/ollama/ollama`. That module is the Ollama server, and the Go vulnerability database lists advisories against the whole module (GO-2025-3557, GO-2025-3558, GO-2025-3559, GO-2025-3582, GO-2025-3689, GO-2025-3695, GO-2025-3824, GO-2025-4251) with no fixed version. The advisories describe the server, but because they cover every package, `govulncheck` reports them as reachable through the `api` client, and no upgrade clears them. CI fails on reachable vulnerabilities, and the project does not exempt findings.

ArchGuard needs only a few of Ollama's REST endpoints, each with a single JSON response.

## Decision

- **The Ollama provider calls Ollama's REST API with `net/http`,** as the Voyage provider calls Voyage, and its tests pin the request and response fields.
- **`go.mod` does not require `github.com/ollama/ollama`.**

## Rules

- Code MUST NOT import `github.com/ollama/ollama` or any of its packages.

## Alternatives considered

- **Exempting the advisories in `govulncheck`.** Rejected: the project fixes findings rather than exempting them.
- **Ollama's OpenAI-compatible `/v1` endpoints through the OpenAI provider.** Rejected: they have no equivalent of `/api/generate`'s `prompt_eval_count`, which token counting needs (`docs/arch/0002-provider-scoped-token-counting.md`).
- **A community Ollama client library.** Rejected: a less-vetted dependency for a few JSON calls.

## Consequences

- An Ollama API change that the official client would have absorbed has to be made here. The endpoints have been stable, and the e2e and unit tests cover the fields ArchGuard sends and reads.
- One dependency tree (the Ollama server's) leaves `go.sum`.
- The module's client signed requests with the local `~/.ollama` key for Ollama's hosted API. ArchGuard never documented that and sends unsigned requests, so Ollama's hosted API is not supported.
