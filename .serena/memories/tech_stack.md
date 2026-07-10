# Tech Stack

- Language: Go 1.25+
- AI Providers: Ollama (default for local, using llama3.2 for LLM and nomic-embed-text for embeddings), OpenAI, Gemini
- Vector Store: Local JSON Index (default) or PostgreSQL + pgvector (remote/CI environments)
- Document Providers: Local Filesystem (default), Atlassian Confluence (via REST API v2 using html-to-markdown)
- Config Format: YAML (`archguard.yaml`)
- ADR Format: Markdown with YAML frontmatter
- CI/CD: GitHub Actions (Composite Action)
- Build/Release: GoReleaser
- Testing: Go standard testing library, `testcontainers-go` for PostgreSQL integration tests