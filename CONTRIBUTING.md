# Contributing to ArchGuard

First off, thank you for taking the time to contribute! ArchGuard is built on the idea that architectural integrity should be automated, not just documented. Whether you are fixing a bug, improving the LLM prompts, or optimizing the vector search, your help is appreciated.

## Getting Started

To begin contributing, you will need Go 1.25 or later and a local instance of Ollama to run the default models. Once your environment is ready, fork the repository and clone it to your local machine.

Run `go mod download` to pull the necessary dependencies, including the tokenizer and YAML parser. You can build the project locally using `go build -o archguard ./cmd/archguard`. Before submitting any changes, ensure that the binary compiles and that you have tested your logic against a local ADR directory.

---

## Project Architecture

The codebase is organized into several internal packages to maintain a strict separation of concerns.

- **cmd/archguard**: This is the entry point. It manages CLI flags and environment variables like `ARCHGUARD_API_KEY`. Keep this layer thin.
- **internal/analysis**: This is the core engine. It coordinates the analysis pipeline, manages the worker pool, and handles file truncation for LLM context windows.
- **internal/index**: This package manages the vector store and ADR parsing. It is responsible for calculating hashes to determine if the index needs a rebuild.
- **internal/llm**: This contains the provider interfaces. If you want to add a new provider (like Anthropic), this is where you would implement the `Provider` interface.

---

## Technical Standards

We follow a minimalist approach to code documentation. Avoid adding conversational or obvious comments to your code. Instead, prioritize clean variable naming and modular design.

For public-facing functions and complex logic, use **Structured Block Commenting** combined with **Explicit Type Documentation**. This ensures that the intent of the logic is clear for future contributors.

### Analysis Pipeline & Performance

- **Vector Search**: ArchGuard relies on cosine similarity to find relevant ADRs. Ensure search logic adheres to the formal definition: $\text{similarity} = \frac{\mathbf{A} \cdot \mathbf{B}}{\|\mathbf{A}\| \|\mathbf{B}\|}$.
- **Parallelism**: The analysis engine uses a worker pool to process files concurrently. The default concurrency is 5, adjustable via `max_concurrency` in the config.
- **Smart Truncation**: To fit within LLM context limits, the engine truncates large files by rolling back to the nearest preceding newline character to avoid breaking code mid-line.
- **Caching**: Analysis results are cached in `.archguard/cache` using a SHA-256 hash of the model name, ADR content, file content, and prompts. This is critical for maintaining performance in local environments.

---

## Testing and Pull Requests

We maintain a robust testing suite that includes unit tests for internal logic and E2E tests for the CLI. Run `go test ./...` to execute the full suite. Our E2E tests utilize a mock provider to verify logic without incurring API costs or requiring a running Ollama instance.

When you are ready to submit your changes, please use **Conventional Commits** for your messages. For example, use `feat: add support for local vector caching` or `fix: handle malformed JSON from LLM`. Pull requests will be reviewed for idiomatic Go patterns and architectural alignment.

### CI and Exit Codes

When contributing to the CLI, ensure that behavioral changes respect standard exit codes:

- **0**: Success (no violations found).
- **1**: Failure (architectural violations detected or environment error).
- **--ci flag**: Changes that result in truncated context should only trigger warnings in CI mode to maintain pipeline stability.
