# Task Completion

Before considering a task complete:
1. Run `go test ./...` to ensure no tests were broken.
2. If configuration changes were made, run `go build ./cmd/archguard` to verify compilation.
3. If ADRs or LLM logic were changed, test with `archguard index` and `archguard check` locally.
