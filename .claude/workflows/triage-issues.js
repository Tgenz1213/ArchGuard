export const meta = {
  name: 'triage-issues',
  description: 'Check open issues against the ticket conventions and propose edits, without changing anything on GitHub',
  whenToUse: 'Run with "all" for every open issue, issue numbers for just those, or both, such as /triage-issues all 354 372',
  phases: [
    { title: 'Select', detail: 'list the open issues and the named ones' },
    { title: 'Triage', detail: 'one agent per issue' },
  ],
}

const CHECKS = [
  'title',
  'template',
  'label',
  'parent-epic',
  'blocking-links',
  'acceptance-criteria',
  'still-true',
  'duplicate',
]

const SELECTION_SCHEMA = {
  type: 'object',
  required: ['issues'],
  properties: {
    issues: {
      type: 'array',
      items: {
        type: 'object',
        required: ['number', 'title', 'state', 'milestone'],
        properties: {
          number: { type: 'integer' },
          title: { type: 'string' },
          state: { type: 'string', enum: ['open', 'closed'] },
          milestone: { type: ['string', 'null'] },
        },
      },
    },
  },
}

const TRIAGE_SCHEMA = {
  type: 'object',
  required: ['findings'],
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        required: ['check', 'severity', 'finding', 'edit', 'evidence'],
        properties: {
          check: { type: 'string', enum: CHECKS },
          severity: { type: 'string', enum: ['required', 'optional'] },
          finding: { type: 'string' },
          edit: { type: 'string' },
          evidence: { type: 'array', items: { type: 'string' } },
        },
      },
    },
  },
}

const EVIDENCE = /[\w./-]+:\d+|#\d+/

const selection = parseSelection(args)

phase('Select')
const selected = await agent(selectPrompt(selection), {
  agentType: 'triager',
  schema: SELECTION_SCHEMA,
  label: 'list issues',
})
if (!selected) {
  return 'Could not list the issues to triage.'
}

const issues = uniqueByNumber(selected.issues)
log(`Triaging ${issues.length} issue(s)`)

phase('Triage')
const results = await pipeline(issues, triage)

return report(issues, results)

function triage(issue) {
  return agent(triagePrompt(issue), {
    agentType: 'triager',
    schema: TRIAGE_SCHEMA,
    label: `#${issue.number}`,
    phase: 'Triage',
  })
}

function parseSelection(input) {
  const words = Array.isArray(input) ? input : String(input ?? 'all').split(/[\s,]+/)
  const tokens = words.map(word => String(word).trim().replace(/^#/, '')).filter(Boolean)

  return {
    all: tokens.length === 0 || tokens.includes('all'),
    numbers: tokens.filter(token => /^\d+$/.test(token)).map(Number),
  }
}

function selectPrompt({ all, numbers }) {
  const wanted = []
  if (all) {
    wanted.push('every open issue in the repository (issues only, not pull requests)')
  }
  if (numbers.length > 0) {
    wanted.push(`these issues, open or closed: ${numbers.map(n => `#${n}`).join(', ')}`)
  }

  return `List ${wanted.join(', plus ')}. For each, return its number, title, state and milestone title (null when it has none). Read only; change nothing on GitHub.`
}

function triagePrompt(issue) {
  const ref = `#${issue.number} ("${issue.title}")`

  if (issue.state === 'closed') {
    return `Issue ${ref} is closed. Check only whether another issue, open or closed, asks for the same thing. Report each duplicate with check "duplicate", the other issue's number in evidence, and the edit that resolves it. Return no other findings. Read only; change nothing on GitHub.`
  }

  return `Triage issue ${ref} against "Ticket & PR Conventions" in CLAUDE.md, the templates in .github/ISSUE_TEMPLATE/ and .claude/skills/file-ticket/SKILL.md. Read the issue, its comments and its parent epic. Check:
- title: Conventional Commits prefix, then a verb.
- template: the matching template's headings.
- label: the label of that template.
- parent-epic: a "Part of #N" issue is a sub-issue of #N.
- blocking-links: a blocker named in the text is a GitHub dependency, when your tools can show dependencies.
- acceptance-criteria: present, and each line passes or fails on its own.
- still-true: main or a merged PR does not already do what it asks, and the files, lines and ADR numbers it cites still say what it claims.
- duplicate: no other open or closed issue asks for the same thing.
Do not report milestones; they are summarised separately.
Report only problems, each with the edit that fixes it. A check that passes or does not apply gets no finding. Mark an edit "required" when it breaks a convention or a claim is false, and "optional" when it only improves the issue. Put a file:line or an issue or PR number you read in evidence; a finding you could not check gets an empty evidence list. Read only; change nothing on GitHub.`
}

function uniqueByNumber(list) {
  const byNumber = new Map()
  for (const issue of list) {
    byNumber.set(issue.number, issue)
  }

  return [...byNumber.values()].sort((a, b) => a.number - b.number)
}

function report(issues, results) {
  const required = []
  const optional = []
  const unverified = []
  const failed = []
  const rows = []
  let withRequired = 0
  let clean = 0

  issues.forEach((issue, index) => {
    const result = results[index]
    if (!result) {
      failed.push(issue.number)
      rows.push(`| #${issue.number} | ${cell(issue.title)} | not triaged |`)
      return
    }

    const findings = issue.state === 'closed' ? result.findings.filter(f => f.check === 'duplicate') : result.findings
    const verified = findings.filter(f => f.evidence.some(e => EVIDENCE.test(e)))
    const unchecked = findings.filter(f => !verified.includes(f))
    const requiredHere = verified.filter(f => f.severity === 'required')
    const optionalHere = verified.filter(f => f.severity === 'optional')

    requiredHere.forEach(f => required.push(editRow(issue, f)))
    optionalHere.forEach(f => optional.push(editRow(issue, f)))
    unchecked.forEach(f => unverified.push(`- #${issue.number} (${f.check}): ${f.finding}`))

    rows.push(`| #${issue.number} | ${cell(issue.title)} | ${status(requiredHere, optionalHere, unchecked)} |`)
    if (requiredHere.length > 0) {
      withRequired++
    }
    if (findings.length === 0) {
      clean++
    }
  })

  const missingMilestone = issues.filter(i => i.state === 'open' && !i.milestone).map(i => i.number)
  const editHeader = '| Issue | Check | Finding | Proposed edit | Evidence |\n|---|---|---|---|---|'

  return [
    '## Summary',
    `${issues.length - failed.length} of ${issues.length} issue(s) triaged: ${withRequired} with required edits, ${clean} with no change.`,
    `**Not triaged (agent failed):** ${numbers(failed)}`,
    `**No milestone:** ${numbers(missingMilestone)}`,
    '## Required edits',
    required.length > 0 ? [editHeader, ...required].join('\n') : 'None.',
    '## Optional edits',
    optional.length > 0 ? [editHeader, ...optional].join('\n') : 'None.',
    '## Unverified',
    unverified.length > 0 ? unverified.join('\n') : 'None.',
    '## Issues',
    ['| Issue | Title | Result |\n|---|---|---|', ...rows].join('\n'),
  ].join('\n\n')
}

function numbers(list) {
  return list.length > 0 ? list.map(n => `#${n}`).join(', ') : 'none'
}

function status(requiredHere, optionalHere, unchecked) {
  if (requiredHere.length + optionalHere.length + unchecked.length === 0) {
    return 'no change'
  }

  return `${requiredHere.length} required, ${optionalHere.length} optional, ${unchecked.length} unverified`
}

function editRow(issue, f) {
  return `| #${issue.number} | ${f.check} | ${cell(f.finding)} | ${cell(f.edit)} | ${cell(f.evidence.join(', '))} |`
}

function cell(text) {
  return String(text).replace(/\|/g, '\\|').replace(/\n+/g, ' ')
}
