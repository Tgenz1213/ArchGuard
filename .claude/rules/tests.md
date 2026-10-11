---
paths:
  - "**/*_test.go"
  - "test/**"
  - "internal/testutil/**"
  - "cmd/archguard-e2e/**"
---

# Tests

How tests are written, tagged and run is in `docs/standards/testing.md`. `.golangci.yml` lists the `integration` and `e2e` tags under `run.build-tags`, so tagged files are linted too.

`cmd/archguard-e2e` is a second binary that passes one `inference.MockProvider` per role to `cli.Execute`'s `ProviderFactories`; `test/e2e` builds and runs it as a subprocess instead of calling real LLM APIs or Ollama.
