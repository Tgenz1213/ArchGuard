package output

type DryRun struct {
	Calls  int `json:"judgment_calls"`
	Cached int `json:"cached"`
}

func (p *Printer) dryRunReport(report Report) {
	report.Gaps = report.sortedGaps()

	p.reportGaps(report.Gaps)
	p.Result("Dry run: %d ADR check(s) would call the model and %d would be answered from the cache; nothing was judged.", report.DryRun.Calls, report.DryRun.Cached)
	p.reportUsage(report.Usage)
}
