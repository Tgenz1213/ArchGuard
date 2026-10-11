---
paths:
  - ".github/**"
  - "action.yml"
  - ".golangci.yml"
  - ".goreleaser.y*ml"
---

# CI and the GitHub Action

## Workflows

CI (`.github/workflows/checks.yml`, called by `pr.yml` on pull requests and `main.yml` on pushes to main) runs `golangci-lint`, then `go test -v -race -cover ./...` and a build. Lint runs golangci-lint `latest` with the linters and settings in `.golangci.yml`. `checks.yml` runs unit, integration and e2e as separate jobs. Those tests pull `pgvector/pgvector` from Docker Hub, so `checks.yml` logs in with the `DOCKERHUB_USERNAME` variable and `DOCKERHUB_TOKEN` secret on pushes to main and PRs authored by the repo owner; other PRs pull anonymously and can hit the rate limit.

`archguard.yml` dogfoods the repo's own Action (`uses: ./`) on the files a push or PR changed, for pushes to main and PRs authored by the repo owner only (it spends the `GEMINI_API_KEY` secret); it copies `.github/archguard-ci.yaml` (Gemini) over `archguard.yaml` on the runner because there is no `--config` flag yet (#269).

## `action.yml`

A composite GitHub Action, not a separate service — it `go install`s `./cmd/archguard` from the action's own checked-out source at run time, optionally installs Ollama and pulls `llama3.2` + `nomic-embed-text` (only when `inputs.provider == 'ollama'`), then runs `archguard check --ci` on the files the event changed (`--since` the PR base commit, or the push's `before` commit; with no usable base, or `scope: all`, it passes `--all`). A clean CI checkout has no uncommitted changes, so `check --ci` without a scope (`--since`, `--all`, `--staged` or paths) is a usage error (`checkCmd.Validate`). Its inputs are `provider`, `scope` (`changed` default, or `all`), `base` and `debug` (`true` adds `--debug`); everything else (model, thresholds, ADR path, exclude patterns) comes from the consumer's own `archguard.yaml`, same as local CLI usage. This is the CI Warn-Open path in `internal/analysis`, not a distinct code path there.
