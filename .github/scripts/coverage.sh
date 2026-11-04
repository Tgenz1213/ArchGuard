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

# Counts come from the profile, not go tool cover's total, which is rounded and would let 79.96% pass as 80.0%.
read -r covered statements < <(awk 'NR > 1 { statements += $2; if ($3 > 0) covered += $2 } END { print covered + 0, statements + 0 }' "$filtered")

total=$(awk -v c="$covered" -v s="$statements" 'BEGIN { printf "%.1f", 100 * c / s }')
echo "Unit test coverage: ${total}% (${covered} of ${statements} statements; minimum ${threshold}%)"

if awk -v c="$covered" -v s="$statements" -v min="$threshold" 'BEGIN { exit !(100 * c < min * s) }'; then
  echo "::error::Unit test coverage ${total}% is under ${threshold}%"
  exit 1
fi
