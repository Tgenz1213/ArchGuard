#!/usr/bin/env bash
# Runs the unit tests and fails when their total statement coverage is under the threshold given as $1.
set -euo pipefail

threshold=${1:?usage: coverage.sh <minimum percent>}
profile=$(mktemp)
filtered=$(mktemp)
trap 'rm -f "$profile" "$filtered"' EXIT

go test -race -coverprofile="$profile" ./...

# Neither is shipped code: one is the mock binary the e2e tests run, the other holds test helpers.
grep -v -e '/cmd/archguard-e2e/' -e '/internal/testutil/' "$profile" > "$filtered"

total=$(go tool cover -func="$filtered" | awk '/^total:/ { sub(/%/, "", $NF); print $NF }')
echo "Unit test coverage: ${total}% (minimum ${threshold}%)"

if awk -v total="$total" -v min="$threshold" 'BEGIN { exit !(total < min) }'; then
  echo "::error::Unit test coverage ${total}% is under ${threshold}%"
  exit 1
fi
