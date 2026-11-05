---
title: Unit tests cover 80% of the code
status: Accepted
scope:
  - ".github/workflows/checks.yml"
  - ".testcoverage.yml"
---

<!-- Write an ADR only for a decision, and keep it short. Never edit it once accepted: a changed decision gets a new ADR that supersedes this one. See docs/standards/guidance.md. -->

# Unit tests cover 80% of the code

## Context

Much of the command line was tested only end to end, so a change could leave code that no fast test runs, and nothing showed it. The end-to-end and integration tests need a built binary or Docker, so they don't run on every local test run and can't stand in for unit tests.

## Decision

- **CI fails when unit tests cover less than 80% of the code's statements.** The bar is one total for the repository, not a floor for each package.
- **Only unit tests count:** the tests that run with no build tag. End-to-end and integration tests don't count toward the bar.
- **Only code that isn't shipped is left out of the measurement:** the mock binary the end-to-end tests run, and the test helpers.
- **The bar is met by writing tests, never by leaving more code out of the measurement or lowering the bar.**

## Rules

- CI MUST fail when unit test coverage of the code's statements is under 80%.
- Coverage MUST be measured with unit tests only, never with end-to-end or integration tests.
- Shipped code MUST NOT be left out of the coverage measurement.

## Alternatives considered

- **A floor for each package.** A small package with little logic would hold up unrelated work, and the gaps that prompted this were in one package that a repository total also catches.
- **Counting end-to-end tests.** They would cover the command line without unit tests, but they don't run with a plain test run, so the number wouldn't match what a developer sees locally.

## Consequences

- New command line code needs unit tests that run it in a temporary repository with mock providers, not only an end-to-end test.
- Coverage is measured on one platform, so code that runs only on another doesn't count toward the total.
