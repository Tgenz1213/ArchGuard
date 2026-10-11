---
paths:
  - "internal/output/**"
---

# `internal/output`

`Printer` is the one place human-readable output is formatted: labels, indentation, per-file `Group`s flushed as one block, progress dots, the end-of-run `Report` and `BaselineReport`, GitHub `::error` annotations, and color. Which writes are primary (a failed write fails the run) and which are best-effort is in `docs/arch/0026-human-output-and-write-failures.md`; color in `docs/arch/0027-terminal-color.md`; the report's contents in `docs/arch/0028-check-report-and-log.md`.

- The report layout is asserted only in this package's tests; other packages assert on the findings (`output.Gaps`, `output.Coverage`). A new kind of gap is a new field in `output.Gaps`.
- `Indented()` is `Group`'s unbuffered sibling, for a header that must print even with nothing under it (the `index` summary).
