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
	var validADRs []ADR
	var stats FetchStats

	err := filepath.Walk(p.dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			stats.Discovered++

			adr, rulesErr, err := parseADRFile(path, p.dirPath, p.idPattern, p.parseOpts)
			if err != nil {
				p.out.Warn("skipping %s: %v", path, err)
				stats.ParseFailed = append(stats.ParseFailed, path)
				return nil
			}

			if isAcceptedStatus(adr.Status, p.acceptedStatuses) {
				validADRs = append(validADRs, *adr)

				if rulesErr != nil {
					p.out.Warn("ignoring rules in %s: %v", path, rulesErr)
					stats.MalformedRules = append(stats.MalformedRules, MalformedRules{RelPath: adr.RelPath, Reason: rulesErr.Error()})
				}
			} else {
				stats.StatusRejected++
			}
		}

		return nil
	})

	if err != nil {
		return nil, FetchStats{}, err
	}

	return validADRs, stats, nil
}
