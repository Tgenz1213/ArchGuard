export const meta = {
  name: 'audit-standards',
  description: 'Audit Go packages against the docs/standards pages and verify each finding, without changing any file',
  whenToUse: 'Run with no arguments or "all" for every Go package, or with package directories, such as /audit-standards internal/output internal/git',
  phases: [
    { title: 'Select', detail: 'list the Go packages' },
    { title: 'Audit', detail: 'one reviewer per package' },
    { title: 'Verify', detail: 'skeptical reviewers, one per batch of findings' },
  ],
}

const PAGES = ['paradigm', 'functions', 'errors', 'naming', 'formatting', 'comments', 'testing']
const PAGE_LIST = PAGES.map(page => `docs/standards/${page}.md`).join(', ')
const BATCH_SIZE = 20

const SELECTION_SCHEMA = {
  type: 'object',
  required: ['packages'],
  properties: {
    packages: { type: 'array', items: { type: 'string' } },
  },
}

const FINDINGS_SCHEMA = {
  type: 'object',
  required: ['findings'],
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        required: ['file', 'line', 'page', 'rule', 'code', 'why'],
        properties: {
          file: { type: 'string' },
          line: { type: 'integer' },
          page: { type: 'string', enum: PAGES.map(page => `${page}.md`) },
          rule: { type: 'string', description: "the rule's bold sentence, verbatim" },
          code: { type: 'string', description: 'the offending code as read' },
          why: { type: 'string', description: 'one sentence on how the code breaks the rule' },
        },
      },
    },
  },
}

const VERDICTS_SCHEMA = {
  type: 'object',
  required: ['verdicts'],
  properties: {
    verdicts: {
      type: 'array',
      items: {
        type: 'object',
        required: ['id', 'verdict', 'reason', 'fix'],
        properties: {
          id: { type: 'integer' },
          verdict: { type: 'string', enum: ['real', 'borderline', 'false'] },
          reason: { type: 'string' },
          fix: { type: 'string', description: 'the change that fixes a real or borderline finding; empty for a false one' },
        },
      },
    },
  },
}

const named = packageArgs(args)

phase('Select')
const packages = named.length > 0 ? named : await listPackages()
if (!packages) {
  return 'Could not list the Go packages to audit.'
}
log(`Auditing ${packages.length} package(s)`)

const results = await pipeline(packages, audit, verify)

return report(packages, results)

function packageArgs(input) {
  const words = Array.isArray(input) ? input : String(input ?? '').split(/[\s,]+/)

  return words.map(word => String(word).trim().replace(/\/+$/, '')).filter(word => word && word !== 'all')
}

async function listPackages() {
  const listed = await agent(
    "Use Glob with the patterns internal/**/*.go and cmd/**/*.go, and return the sorted, distinct directories that hold the matched files. Each directory is one Go package. Change nothing.",
    { agentType: 'reviewer', effort: 'low', schema: SELECTION_SCHEMA, label: 'list packages' },
  )

  return listed ? [...new Set(listed.packages)].sort() : null
}

async function audit(pkg) {
  const audited = await agent(auditPrompt(pkg), {
    agentType: 'reviewer',
    schema: FINDINGS_SCHEMA,
    label: `audit ${pkg}`,
    phase: 'Audit',
  })

  return audited ? uniqueFindings(audited.findings).map(finding => ({ ...finding, pkg })) : null
}

async function verify(findings, pkg) {
  if (!findings) {
    return null
  }

  const batches = []
  for (let start = 0; start < findings.length; start += BATCH_SIZE) {
    batches.push(findings.slice(start, start + BATCH_SIZE))
  }

  const judged = await parallel(batches.map((batch, index) => () => agent(verifyPrompt(batch), {
    agentType: 'reviewer',
    schema: VERDICTS_SCHEMA,
    label: `verify ${pkg} ${index + 1}/${batches.length}`,
    phase: 'Verify',
  })))

  return batches.flatMap((batch, index) => withVerdicts(batch, judged[index]))
}

function auditPrompt(pkg) {
  return `Audit the Go package in \`${pkg}\` against this repository's coding standards: only the .go files directly in that directory, test files included, not subdirectories.

1. Read each of these pages in full: ${PAGE_LIST}. Each rule is a bold sentence in a "## Rules" list.
2. List the package's files with Glob (pattern \`${pkg}/*.go\`) and read every one in full.
3. Report every place the code breaks a rule. Skip a breach the page says a linter already enforces. Test files follow the pages too, except where a page exempts them.

For each finding give the file path from the repo root, the line, the page file name, the rule's bold sentence verbatim, the code you read and one sentence on why it breaks the rule. Report only what you read. Return an empty list if the package is clean. Read only; change nothing.`
}

function verifyPrompt(batch) {
  const numbered = batch.map((finding, id) => ({ id, ...finding }))

  return `Each finding below claims that this repository's code breaks a rule in docs/standards/. Be skeptical: for each one, open the cited page and the cited file and line, and judge it.

- "real": the code breaks the rule as the page words it, and no exemption on the page applies.
- "borderline": the page leaves this case to judgement.
- "false": the rule is not on that page, the code at that line is not what the finding says, or the code does not break the rule.

Return one verdict per id, with one sentence of reason, and for a real or borderline finding the fix. Read only; change nothing.

Findings (JSON):
${JSON.stringify(numbered, null, 1)}`
}

function uniqueFindings(findings) {
  const byKey = new Map()
  for (const finding of findings) {
    byKey.set(`${finding.file}:${finding.line}:${finding.page}:${finding.rule}`, finding)
  }

  return [...byKey.values()]
}

function withVerdicts(batch, judged) {
  const verdicts = new Map((judged?.verdicts ?? []).map(verdict => [verdict.id, verdict]))

  return batch.map((finding, id) => {
    const verdict = verdicts.get(id)
    if (!verdict || !cited(finding)) {
      return { ...finding, verdict: 'unverified', reason: verdict ? 'no file:line or page' : 'not verified', fix: '' }
    }

    return { ...finding, verdict: verdict.verdict, reason: verdict.reason, fix: verdict.fix }
  })
}

function cited(finding) {
  return finding.file.trim() !== '' && finding.line > 0 && PAGES.includes(finding.page.replace(/\.md$/, ''))
}

function report(packages, results) {
  const failed = packages.filter((_, index) => !results[index])
  const findings = results.filter(Boolean).flat()
  const byVerdict = verdict => findings.filter(finding => finding.verdict === verdict)
  const real = byVerdict('real')

  return [
    '## Summary',
    `${packages.length - failed.length} of ${packages.length} package(s) audited: ${real.length} real, ${byVerdict('borderline').length} borderline, ${byVerdict('false').length} false, ${byVerdict('unverified').length} unverified.`,
    `**Not audited (agent failed):** ${failed.length > 0 ? failed.join(', ') : 'none'}`,
    '## Real, by package',
    realSections(packages, real),
    '## Borderline',
    lines(byVerdict('borderline'), finding => `${location(finding)} ${finding.reason} **Fix:** ${finding.fix}`),
    '## Unverified',
    lines(byVerdict('unverified'), finding => `${location(finding)} ${finding.why} (${finding.reason})`),
    '## False',
    lines(byVerdict('false'), finding => `${location(finding)} ${finding.reason}`),
  ].join('\n\n')
}

function realSections(packages, real) {
  const sections = []
  for (const pkg of packages) {
    const inPackage = real.filter(finding => finding.pkg === pkg)
    if (inPackage.length > 0) {
      sections.push(`### \`${pkg}\` (${inPackage.length})`)
      sections.push(lines(inPackage, finding => `${location(finding)} ${finding.why} **Fix:** ${finding.fix}`))
    }
  }

  return sections.length > 0 ? sections.join('\n\n') : 'None.'
}

function lines(findings, describe) {
  return findings.length > 0 ? findings.map(finding => `- ${describe(finding)}`).join('\n') : 'None.'
}

function location(finding) {
  return `\`${finding.file}:${finding.line}\` [${finding.page}] ${finding.rule}`
}
