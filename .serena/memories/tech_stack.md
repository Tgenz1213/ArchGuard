# Tech Stack

- Language: Go 1.27 (CI runs 1.27)
- AI Providers: Ollama (default for local, using llama3.2 for LLM and nomic-embed-text for embeddings), OpenAI, Gemini; Claude for chat only and Voyage for embeddings only
- Vector Store: Local JSON Index (default) or PostgreSQL + pgvector (remote/CI environments)
- Document Providers: Local Filesystem (default), Atlassian Confluence (via REST API v2 using html-to-markdown)
- Config Format: YAML (`archguard.yaml`)
- CLI Parsing: `kong`
- ADR Format: Markdown with YAML frontmatter
- CI/CD: GitHub Actions (Composite Action)
- Build/Release: GoReleaser
- Lint: golangci-lint (config in `.golangci.yml`)
- Testing: Go standard testing library, `testcontainers-go` for PostgreSQL integration tests
