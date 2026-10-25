---
name: file-ticket
description: Draft and file a GitHub issue for this repo with testable acceptance criteria, the right template, labels, milestone and parent epic. Use when asked to file, open, write or draft a ticket or issue.
---

# File a ticket

The conventions are in `CLAUDE.md` under "Ticket & PR Conventions". Read them first. To check issues that are already open against them, run `/triage-issues` instead.

1. **Search for a duplicate** among open and closed issues before writing anything.
2. **Pick the template** in `.github/ISSUE_TEMPLATE/`: bug report for broken behaviour, feature request for everything else. Use its headings and its label.
3. **Write the title** in Conventional Commits form, starting with a verb after the type, for example `fix: count the ADRs a build embeds`.
4. **Write the acceptance criteria** as checkboxes that each pass or fail on their own: an exit code, a behaviour, a named test. Rewrite any line that needs judgement to decide.
5. **Show the draft** to the person and wait for their approval before filing.
6. **File it.** In a cloud session, `gh` is not installed; use the GitHub connector's issue tools instead.
   - Set the same milestone as its parent epic, if it has one.
   - Add it as a sub-issue of the epic, and tick or add its line in the epic's checklist.
   - Link a blocking issue with GitHub's issue dependencies, not only in prose.
7. **Reply with the issue link.**
