---
paths:
  - "**/*_test.go"
  - "test/**"
  - "internal/testutil/**"
  - "cmd/archguard-e2e/**"
---

# Tests

How tests are written, tagged and run is in [testing.md](../../docs/standards/testing.md).

- **A new build tag is added to `run.build-tags` in `.golangci.yml`,** or the files behind it are never linted.
