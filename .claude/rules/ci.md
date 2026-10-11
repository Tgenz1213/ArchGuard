---
paths:
  - ".github/**"
  - "action.yml"
  - ".golangci.yml"
  - ".goreleaser.y*ml"
---

# CI and the GitHub Action

## Workflows

`checks.yml` (called by `pr.yml` on pull requests and `main.yml` on pushes to main) runs lint, then unit, integration and e2e tests as separate jobs, a build, and a release-config job. The lint job first fails when `go mod tidy -diff` finds changes or when `action.yml`'s `go-version` differs from `go.mod`'s, then runs golangci-lint `latest` with `.golangci.yml`. The unit and e2e jobs run on Ubuntu, Windows and macOS. The integration tests pull `pgvector/pgvector` from Docker Hub, so they stay on Ubuntu; `checks.yml` logs in with the `DOCKERHUB_USERNAME` variable and `DOCKERHUB_TOKEN` secret on pushes to main and on PRs by the repo owner, and other PRs pull anonymously and can hit the rate limit. The release-config job runs `goreleaser check`, which also fails on deprecated keys, and builds all six release targets with `goreleaser build --snapshot` when the change touches `.goreleaser.yaml`, `release.yml`, `go.mod`, `go.sum` or `cmd/`.

Every job reads the Go version through `.github/actions/setup-go`, which installs the latest patch of `go.mod`'s major.minor. `actions/setup-go`'s own `go-version-file: go.mod` would pin the exact `go 1.27.0` line instead. `action.yml` keeps its own literal `go-version`, because it runs against the consumer's checkout, and the lint job keeps the two equal.

`vulncheck.yml` runs `govulncheck`, which fails only on a vulnerability in code ArchGuard calls. `pr.yml`, `main.yml` and the Monday `weekly.yml` call it. `pr-title.yml` fails a PR whose title is not a Conventional Commit, because the squash-merged title is the commit on main.

`pr.yml` cancels a PR's earlier run when a new push arrives. `main.yml` has no concurrency group on purpose: a group would let a later push cancel a pending main run. Each job sets `timeout-minutes`; set it on any new job.

`archguard.yml` dogfoods the repo's own Action (`uses: ./`) on the files a push or PR changed, for pushes to main and PRs by the repo owner only, since it spends the `GEMINI_API_KEY` secret. It copies `.github/archguard-ci.yaml` over `archguard.yaml` on the runner because the Action has no `config` input yet (#327).

## `action.yml`

A composite Action that `go install`s `./cmd/archguard` from its own checkout, installs Ollama and pulls `llama3.2` and `nomic-embed-text` only when `inputs.provider == 'ollama'`, then runs `archguard check --ci` with `--since` the PR base or the push's `before` commit, or `--all` when there is no usable base or `scope: all`. Its inputs are `provider`, `scope` (`changed` or `all`), `base` and `debug`; everything else comes from the consumer's `archguard.yaml`. A clean CI checkout has no uncommitted changes, so `check --ci` without `--since`, `--all`, `--staged` or paths is a usage error.
