#!/bin/bash
# Turns on the git hooks in .githooks for every session.
# In cloud sessions only, also installs a golangci-lint built with go.mod's Go
# version, because the cloud image's copy is built with an older Go and can't
# lint this module.
set -euo pipefail

git -C "$CLAUDE_PROJECT_DIR" config core.hooksPath .githooks

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

go_version=$(sed -n 's/^go \([0-9.]*\)$/\1/p' "$CLAUDE_PROJECT_DIR/go.mod")
gobin=$(go env GOPATH)/bin
lint="$gobin/golangci-lint"

if ! "$lint" version 2>/dev/null | grep -q "built with go$go_version "; then
  GOTOOLCHAIN="go$go_version" GOBIN="$gobin" \
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
fi

echo "export PATH=\"$gobin:\$PATH\"" >> "$CLAUDE_ENV_FILE"
