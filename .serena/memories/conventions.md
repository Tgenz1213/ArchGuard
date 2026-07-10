# Conventions

- Go idioms: Standard Go layout with `cmd/` for entrypoints and `internal/` for private packages.
- ADR files must have YAML frontmatter with `title`, `status`, and optional `scope` glob.
- Violations can be suppressed locally with `// archguard-ignore: [ADR_ID]`.
- Smart truncation is used to prevent exceeding LLM context windows (rolling back to nearest newline).
- Delta Indexing: The vector store explicitly skips LLM embedding API calls for ADRs whose content, title, and status have not changed.
- Postgres Vector Store: Uses pgvector with an HNSW index (`vector_cosine_ops`), executing `ON CONFLICT DO UPDATE` upserts and conditional `REINDEX` queries for safe graph maintenance.