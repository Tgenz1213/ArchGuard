---
title: Use Golang for Backend Services
status: Accepted
scope: "**"
---

# Use Golang for Backend Services

## Decision

1. **Mandatory Language**: All executable backend logic must be implemented in **Go (Golang)**.
2. **Permitted Non-Go Files**:
   - Configuration: `.yaml`, `.yml`, `.json`, `.toml`
   - Automation: `.sh`, `.ps1`
   - Documentation: `.md`
3. **Out of Scope**: This ADR does not dictate _how_ Go is written (coding style, concurrency patterns, or library choices). It only mandates the _presence_ of the Go language itself.

## Consequences

- Standardized toolchain.
- Code must pass through the Go compiler.
