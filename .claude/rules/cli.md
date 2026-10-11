---
paths:
  - "internal/cli/**"
  - "cmd/archguard/**"
---

# `internal/cli`

- **Exit codes are a public contract.** The GitHub Action and users' scripts branch on them. A change to a code or to which code wins also changes the exit code list in `README.md` and its tests.
- **Paths given on the command line are resolved before the `chdir` to the repo root.** A new positional path or path-valued flag goes through `checkCmd.resolvePaths` (or its command's equivalent), and a positional that is not a path, such as `--since`'s ref, stays out of it.
- **`.` anywhere in `check`'s paths means the whole repo,** so `check foo.go .` scans everything.
- **Everything `check` runs that prints takes the run's `log` printer,** an index rebuild included. Only the report goes to the `report` printer, or `--format json` stops being one JSON document (`docs/arch/0028-check-report-and-log.md`).
- **`checkRun` wraps the providers in their `telemetry` meters before anything runs,** so an index rebuild's calls are counted in the usage too (`docs/arch/0035-run-recorder-seam.md`).
