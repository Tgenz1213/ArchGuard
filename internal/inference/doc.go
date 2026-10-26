/*
Package inference talks to the model providers: Ollama, OpenAI, Gemini, Claude
and Voyage, and to Jev, which answers yes/no questions about a text.

Chat and embedding are separate roles. A provider implements only the roles
its vendor offers, so Claude only chats and Voyage only embeds, and code
depends on the role it uses rather than on a provider. It also holds the
prompts that ask whether a file breaks an ADR and how to fix a violation, and
parses the model's answers.

It does not decide which ADRs to ask about or keep answers between runs; that
is package analysis, with package cache. Package cli builds the providers from
the configuration.

Terms:

  - Task role: whether an embedding is for a stored ADR (a document) or for a
    file being searched (a query). Each provider maps it onto its backend's
    convention or ignores it.
  - Usage: the tokens a backend reported for a call. A call whose backend
    reported nothing is marked as unreported, never counted as zero.
*/
package inference
