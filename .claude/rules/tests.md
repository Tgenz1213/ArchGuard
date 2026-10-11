---
paths:
  - "**/*_test.go"
  - "test/**"
  - "internal/testutil/**"
  - "cmd/archguard-e2e/**"
---

# Tests

How tests are written, tagged and run is in `docs/standards/testing.md`. Tests are split by build tag; `.golangci.yml` lists both tags under `run.build-tags` so the tagged files are still linted.

`cmd/archguard-e2e/main.go` is a second binary entrypoint that supplies two distinct `inference.MockProvider` instances (one per role) to `cli.Execute`'s `ProviderFactories` so `test/e2e/e2e_test.go` can build and exec that binary as a subprocess instead of hitting real LLM APIs or Ollama, including for configs where `llm.provider` and `vector_store.provider` differ.
