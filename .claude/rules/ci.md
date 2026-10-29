---
paths:
  - ".github/**"
  - "action.yml"
  - ".golangci.yml"
  - ".goreleaser.y*ml"
---

# CI and the GitHub Action

- **Every job gets Go through `.github/actions/setup-go`.** `actions/setup-go`'s own `go-version-file: go.mod` would pin the exact patch in `go.mod` instead of the latest one.
- **`action.yml` keeps a literal `go-version`,** because it runs in the consumer's checkout, where `go.mod` is theirs. A Go version bump in `go.mod` also changes it; the lint job fails while the two differ.
- **`main.yml` has no concurrency group on purpose.** A group would let a later push cancel a pending run on main.
- **Every job sets `timeout-minutes`.** Set it on any new job.
- **A job that spends a secret runs only for pushes to main and PRs by the repo owner or the agent's GitHub App, named by the `AGENT_BOT_LOGIN` repository variable.** A fork's PR cannot read secrets. The Docker Hub login follows the same rule, so other PRs pull `pgvector/pgvector` anonymously and can hit the rate limit.
- **CI's own `archguard check` reads `.github/archguard-ci.yaml`,** not `archguard.yaml`, which stays on Ollama for local use. A setting meant for CI goes there.
- **A clean CI checkout has no uncommitted changes,** so `check --ci` with no `--since`, `--all`, `--staged` or paths is a usage error. A change to how `action.yml` picks its scan must always pass one.
