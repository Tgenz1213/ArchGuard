# Guidance

## Rules

- **Put each fact in the home for its kind.** The table below says where. A fact in the wrong home is moved, not copied.

  | Need | Home |
  |---|---|
  | Applies to every task | `CLAUDE.md` |
  | Invariant, footgun or "when you change X, also change Y" for one code area | That area's file in `.claude/rules/` |
  | What a package is for, what it does not own, its key terms | The package's `doc.go` comment |
  | How a user installs, runs or configures ArchGuard | The root `README.md` |
  | Why a design is the way it is | An ADR in `docs/arch/` |
  | How Go is written | `docs/standards/` |
  | Must always happen, or a command to allow or deny | A hook or permission in `.claude/settings.json` |
  | Checkable by a tool | A lint rule or a test |
  | Recurring multi-step procedure | A skill in `.claude/skills/` |
  | The same check fanned out over many items, rerunnable | A workflow in `.claude/workflows/` |
  | What the code does | Nowhere; the code is the source |

- **State a fact once.** Every other place points to its home. Two copies drift apart, and a reader cannot tell which one is current.
- **Never restate what the code does.** No call flows, no "X calls Y, then Z", no file inventories and no field lists. They go stale with the next refactor, and a reader who needs them reads the code.
- **Do not hardcode a value that changes.** Name the setting or constant instead of copying its default, limit, threshold or version.
- **Move a rule to a tool once a tool can check it.** A lint rule, test, hook or permission enforces it every time; the prose is then deleted.
- **A rules file holds only invariants, footguns and "also change Y" pointers.** A file left with nothing else is deleted, and a file that only points elsewhere is folded into a neighbour.
- **Give each package a `doc.go` comment, not a `README.md`.** It sits next to the code and `go doc` prints it. What it may hold is in [comments.md](comments.md).
- **Write an ADR only for a decision, and never edit it once accepted.** Keep it short: the context, the decision and its rules, the alternatives rejected and the consequences. A change in behaviour never requires editing an ADR; a changed decision gets a new ADR that supersedes the old one ([Fowler](https://martinfowler.com/bliki/ArchitectureDecisionRecord.html)).
- **Keep personal settings out of the repository.** A preference that is one person's belongs in their `~/.claude`; a practice the repository needs on every machine belongs here.
