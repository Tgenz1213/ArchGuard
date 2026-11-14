---
paths:
  - "internal/inference/**"
  - "internal/atomicfile/**"
  - "internal/output/**"
  - "internal/config/**"
  - "internal/index/**"
---

# Wide packages

- **An exported identifier or interface method added, removed or re-typed here reaches every package that imports this one, including `cmd/archguard-e2e` and the tests' mocks.** Before changing one, name the dependents that need it and why the change can't live in `internal/analysis` or `internal/cli`, which few packages import. If a feature needs it and the ticket didn't say so, stop and ask.
- **A second method on an interface that already has one for the same capability is a smell** (`inference.Asker`): widen the existing signature, or make the second a separate interface only its one caller sees.
- **A new package that many others import gets its path added to this file's `paths`.** The `Wide API` job in `pr.yml` warns about a wide package by its computed reach, not by this list (`go doc ./internal/wideapi`).
