---
paths:
  - "internal/index/**"
  - "docker-compose.yml"
---

# `internal/index`

- **Scope, threshold and top-K filtering live once, in `rank.go`, and both stores call it.** Change them there, never in one backend, or the two stores return different ADRs (`docs/arch/0007-scope-filtered-before-topk-similarity.md`).
- **Both stores embed through `embed.go`.** A change to how ADRs are embedded is made there once.
- **Match globs only with `index.MatchGlob`.** `internal/analysis` uses it for `exclude_patterns` too, so there is one matcher.
- **Print diagnostics through the store's or provider's `out` printer, never `fmt.Print*` or `os.Stdout`,** or they corrupt `check --format json` (`docs/arch/0015-index-diagnostic-writer.md`).
- **`CompositeProvider.GetADRs` fails only when every provider fails.** One failing source, such as a flaky Confluence, prints a warning and the run goes on, so a clean `check` does not mean every ADR source was reached.
- **What is rebuilt automatically differs by store.** `check` rebuilds a stale `LocalStore`, but its hash does not cover the frontmatter apart from rules, so an edit that only changes an ADR's `title`, `status`, `scope` or `similarity_threshold` (or `analysis.frontmatter_mappings`) needs an explicit `archguard index`. `check` never rebuilds a `PgStore`, so every ADR change does.
- **`PgStore` caches the project's ADRs for the run, and only `BuildIndex` drops the cache.** An `archguard index` run by another process is not seen until the next run.
- **`PgStore.Load` and `PgStore.BuildIndex` both ensure the schema,** because `archguard index` calls `BuildIndex` without `Load`.
- **`REINDEX INDEX CONCURRENTLY` cannot run twice on one index at once, and needs up to twice the index's disk space while it runs.** When two builds both cross `vector_store.reindex_threshold`, the second gets a Postgres error, printed as a warning, and the next build retries.
- **`pgvector_bench_test.go` is a benchmark behind the `integration` tag, so no test job runs it.** Run it when you change `PgStore` search: `go test -tags integration -bench=BenchmarkPgStoreSearch_ProjectFiltering -run ^$ -benchtime=1x -v ./internal/index` (needs Docker).
