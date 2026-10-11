---
paths:
  - "internal/index/**"
  - "docker-compose.yml"
---

# `internal/index`

ADR ingestion and the vector store.

## ADR providers

`LocalProvider` reads `analysis.adr_path`, `ConfluenceProvider` reads Confluence (REST API v2), and `CompositeProvider` fetches from all of them concurrently and merges. `GetADRs` also returns `FetchStats`, which `health.go` turns into `archguard index`'s corpus summary (`docs/arch/0010-index-corpus-health-reporting.md`). Parse settings are set on each provider after construction (`SetIDPattern`, `SetFrontmatterMappings`, `SetRulesHeading`, `SetPrinter`) and travel to the shared parser as `ParseOptions`; see `docs/arch/0012-configurable-adr-id-extraction-pattern.md`, `docs/arch/0021-configurable-frontmatter-field-mappings.md` and `docs/arch/0023-adr-screening-rules.md`.

## Vector stores

`NewVectorStore` returns `PgStore` (Postgres and pgvector, HNSW) when `vector_store.connection_string` is set, otherwise `LocalStore` (`.archguard/index.json`).

- Scope, threshold and top-K filtering live once, in `rank.go`, and both backends call them, so they cannot diverge. Scope is filtered before the top-K cut, and `PgStore` fetches up to `MaxSearchCandidates` (1000) rows and filters in Go because SQL can't evaluate a `doublestar` glob (`docs/arch/0007-scope-filtered-before-topk-similarity.md`). An ADR's own `similarity_threshold` goes through `meetsThreshold` (`docs/arch/0011-per-adr-similarity-threshold-override.md`).
- `SearchRejected`, `SearchTruncated` and `SearchWithDebugInfo` exist for `--debug` only (`docs/arch/0009-debug-visibility-for-rejected-adr-candidates.md`, `docs/arch/0019-single-query-consistency-for-debug-diagnostics.md`).
- `ScopedADRs` returns scope-matched candidates without an embedding. `PgStore` caches the project's rows after the first call and `BuildIndex` drops the cache, so an `archguard index` run by another process isn't seen until the next run.
- Glob matching is `index.MatchGlob`, which `internal/analysis` also uses for `exclude_patterns`; there is one matcher.
- Every diagnostic goes through the store's or provider's `out *output.Printer`, never `fmt.Print*` or `os.Stdout`, or it corrupts `check --format json` (`docs/arch/0015-index-diagnostic-writer.md`).

## Building the index

Both stores embed through `embed.go`, so a change to how ADRs are embedded is made there once; `PgStore` plugs its upsert in as the `persist` hook. An ADR is re-embedded when no stored ADR has its `RelPath`, or the stored one's `Content`, `Title` or `Status` differs. One ADR failing to embed is skipped, not fatal; a cancelled context or every ADR failing is (`docs/arch/0008-buildindex-partial-success.md`).

**Staleness detection differs by backend.**

- `LocalStore`: `check` compares the saved model name, dimension and hash with the current ones, and rebuilds on any mismatch or a missing index file. `CalculateHash` covers the model name and each ADR's `RelPath`, body `Content`, `ID` and, when it has any, rules; it does not cover the rest of the frontmatter.
- `PgStore`: `CalculateHash` is the constant `"remote"` and `Load` compares nothing, so `check` never rebuilds a Postgres index. `Load` and `BuildIndex` both call `ensureSchema`, because `archguard index` calls `BuildIndex` without `Load`. `BuildIndex` syncs `adr_id`, `scope`, `similarity_threshold` and `rules` without re-embedding.
- So with `LocalStore`, a change that only alters what `Title`, `Status`, `scope` or `similarity_threshold` resolve to (a frontmatter edit, or `analysis.frontmatter_mappings`) needs an explicit `archguard index`; with `PgStore`, every ADR change does.

## Postgres and HNSW

- `PgStore.BuildIndex` reindexes `archguard_adrs_embedding_idx` once churn (embedded plus deleted, over total) crosses `vector_store.reindex_threshold` (default `0.20`), with `REINDEX INDEX CONCURRENTLY` unless `vector_store.reindex_concurrently` is false. `vector_store.reindex_enabled: false` turns it off.
- `hnsw.iterative_scan = 'relaxed_order'` is set on every connection when pgvector is 0.8.0 or newer; an older pgvector gets a one-time warning and the run continues with lower recall (`docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md`).
- `pgvector_bench_test.go` measures project-filtered recall and latency (#44). It is a benchmark, so `go test ./...` never runs it: `go test -tags integration -bench=BenchmarkPgStoreSearch_ProjectFiltering -run ^$ -benchtime=1x -v ./internal/index` (requires Docker).

## Footguns

- **`CompositeProvider.GetADRs` only fails when every provider fails.** One failing source (a flaky Confluence) prints a warning and the run continues on the rest, so a clean `check` doesn't mean every ADR source was reached.
- **`REINDEX INDEX CONCURRENTLY` can't run twice on one index at once, and needs up to twice the index's disk space while it runs.** A second concurrent `BuildIndex` that crosses the threshold gets a Postgres error, which is printed as a warning; the next build retries.
- **The pgvector image is pinned to `pgvector/pgvector:0.8.7-pg16` in both `docker-compose.yml` and `pgvector_integration_test.go`.** The floating `pg16` tag would change the pgvector version, and with it `hnsw.iterative_scan` support, on a routine pull. To bump it, change both files to the same `<pgvector-version>-pg<postgres-major>` tag and keep it at or above `IterativeScanSupportedVersion` (0.8.0).
