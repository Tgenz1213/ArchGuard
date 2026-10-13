package index

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tgenz1213/archguard/internal/output"
)

type LocalProvider struct {
	dirPath          string
	acceptedStatuses []string
	idPattern        *regexp.Regexp
	parseOpts        ParseOptions
	out              *output.Printer
}

func NewLocalProvider(dirPath string, acceptedStatuses []string) *LocalProvider {
	return &LocalProvider{
		dirPath:          dirPath,
		acceptedStatuses: acceptedStatuses,
	}
}

func (p *LocalProvider) SetIDPattern(re *regexp.Regexp) {
	p.idPattern = re
}

func (p *LocalProvider) SetFrontmatterMappings(mappings map[string]string) {
	p.parseOpts.FrontmatterMappings = mappings
}

func (p *LocalProvider) SetRulesHeading(heading string) {
	p.parseOpts.RulesHeading = heading
}

func (p *LocalProvider) SetPrinter(out *output.Printer) {
	p.out = out
}

func (p *LocalProvider) GetADRs(ctx context.Context) ([]ADR, FetchStats, error) {
	collector := adrCollector{out: p.out, acceptedStatuses: p.acceptedStatuses}

	var idPatternMatched bool

	err := filepath.Walk(p.dirPath, func(path string, stat os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if stat.IsDir() || !strings.HasSuffix(stat.Name(), ".md") {
			return nil
		}

		collector.stats.Discovered++

		if p.idPattern != nil && p.idPattern.MatchString(stat.Name()) {
			idPatternMatched = true
		}

		adr, rulesErr, err := parseADRFile(path, p.dirPath, p.idPattern, p.parseOpts)
		collector.add(adr, rulesErr, err, path, path)

		return nil
	})

	if err != nil {
		return nil, FetchStats{}, err
	}

	if p.idPattern != nil && collector.stats.Discovered > 0 && !idPatternMatched {
		p.out.Warn("analysis.adr_id_pattern %q matches none of the %d ADR files in %s; every ID falls back to the filename up to its first hyphen", p.idPattern, collector.stats.Discovered, p.dirPath)
	}

	return collector.adrs, collector.stats, nil
}
