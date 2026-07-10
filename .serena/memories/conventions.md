# Conventions

- Go idioms: Standard Go layout with `cmd/` for entrypoints and `internal/` for private packages.
- ADR files must have YAML frontmatter with `title`, `status`, and optional `scope` glob.
- Violations can be suppressed locally with `// archguard-ignore: [ADR_ID]`.
- Smart truncation is used to prevent exceeding LLM context windows (rolling back to nearest newline).
