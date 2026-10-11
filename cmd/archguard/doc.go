/*
ArchGuard checks changed code against the rules in your Architectural Decision
Records (ADRs). It embeds the ADRs in a vector index, finds the ones relevant to
each changed file, and asks an LLM (Ollama, OpenAI, Gemini or Claude) whether the
file breaks each one.

Install it with:

	go install github.com/tgenz1213/archguard/cmd/archguard@latest

Usage:

	archguard <command> [flags]

# Commands

	init   Set up ArchGuard in the current repository: archguard.yaml and an ADR directory.
	index  Rebuild the ADR index. Run it after adding or editing an ADR.
	check  Check code for violations. With no arguments it checks uncommitted changes;
	       --staged, --all, --since REF or paths choose what else to check.

Run archguard <command> --help for a command's flags. A violation exits with
code 4, so check can gate a commit or a CI job.

Configuration (archguard.yaml), the ADR format, exit codes and the GitHub Action
are documented in the [repository].

[repository]: https://github.com/tgenz1213/archguard
*/
package main
