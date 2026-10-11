---
paths:
  - ".github/**"
  - "action.yml"
  - ".golangci.yml"
  - ".goreleaser.y*ml"
---

# CI and the GitHub Action

## Workflows

`checks.yml` (called by `pr.yml` on pull requests and `main.yml` on pushes to main) runs lint, then unit, integration and e2e tests as separate jobs, and a build. Lint runs golangci-lint `latest` with `.golangci.yml`. The integration and e2e tests pull `pgvector/pgvector` from Docker Hub; `checks.yml` logs in with the `DOCKERHUB_USERNAME` variable and `DOCKERHUB_TOKEN` secret on pushes to main and on PRs by the repo owner, and other PRs pull anonymously and can hit the rate limit.

`archguard.yml` dogfoods the repo's own Action (`uses: ./`) on the files a push or PR changed, for pushes to main and PRs by the repo owner only, since it spends the `GEMINI_API_KEY` secret. It copies `.github/archguard-ci.yaml` over `archguard.yaml` on the runner because there is no `--config` flag yet (#269).

## `action.yml`

A composite Action that `go install`s `./cmd/archguard` from its own checkout, installs Ollama and pulls `llama3.2` and `nomic-embed-text` only when `inputs.provider == 'ollama'`, then runs `archguard check --ci` with `--since` the PR base or the push's `before` commit, or `--all` when there is no usable base or `scope: all`. Its inputs are `provider`, `scope` (`changed` or `all`), `base` and `debug`; everything else comes from the consumer's `archguard.yaml`. A clean CI checkout has no uncommitted changes, so `check --ci` without `--since`, `--all`, `--staged` or paths is a usage error.
