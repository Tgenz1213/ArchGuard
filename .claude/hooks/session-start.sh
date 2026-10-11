#!/bin/bash
# Installs a golangci-lint built with go.mod's Go version, because the cloud
# image's copy is built with an older Go and can't lint this module.
# Cloud sessions only; a local session exits at once.
set -euo pipefail

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
