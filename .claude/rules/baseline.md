---
paths:
  - "internal/baseline/**"
  - "archguard-baseline.json"
---

# `internal/baseline`

`archguard-baseline.json` is a committed snapshot at the repo root (`baseline.Path`, not configurable): `check --update-baseline` rewrites it whole, and every other `check` loads it. Suppression is keyed on `(ADR ID, file)` and lasts while the entry's `QuotedCode` is still in the file, so a second violation of a baselined pair is hidden too (`docs/arch/0006-violation-baseline-file.md`). `Reason` is informational, follows the same key, and `--baseline-reason` overwrites it on every entry the run collects (`docs/arch/0013-baseline-entry-reason.md`).
