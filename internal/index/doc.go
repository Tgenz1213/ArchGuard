/*
Package index turns Architectural Decision Records into an index that can say
which ADRs apply to a changed file.

It reads ADRs from their sources (a local directory or Confluence), parses each
one's frontmatter, scope and rules, embeds it, and keeps the result in a vector
store: a JSON file on disk, or Postgres with pgvector. A store answers which
ADRs a file could break: those whose scope covers the file, ranked by how
similar each is to the change.

It does not judge code. The scoring nodes that choose which candidates reach
the model, and the question of whether a file breaks an ADR, belong to package
analysis. Models are reached through package inference. A store reports why a
saved index is stale, and package cli decides when to rebuild it.

Terms:

  - Scope: the globs in an ADR's frontmatter naming the files it governs. An
    ADR is never a candidate for a file outside its scope.
  - Rules: short statements of what violating code looks like, from an ADR's
    Rules section or its frontmatter.
  - Similarity threshold: the score below which an ADR is not a candidate. An
    ADR's frontmatter can set its own.
  - Project: the name that keeps repositories apart in one shared Postgres
    index.
*/
package index
