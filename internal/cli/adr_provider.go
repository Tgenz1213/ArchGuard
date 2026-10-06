package cli

import (
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
)

func newADRProvider(setup runSetup, out *output.Printer) *index.CompositeProvider {
	cfg := setup.cfg

	localProvider := index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses)
	localProvider.SetIDPattern(setup.adrIDPattern)
	localProvider.SetFrontmatterMappings(setup.frontmatterMappings)
	localProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
	localProvider.SetPrinter(out)

	providers := []index.Provider{localProvider}

	if cfg.Analysis.Confluence.Enabled {
		confluenceProvider := index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		)
		confluenceProvider.SetFrontmatterMappings(setup.frontmatterMappings)
		confluenceProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
		confluenceProvider.SetPrinter(out)

		providers = append(providers, confluenceProvider)
	}

	adrProvider := index.NewCompositeProvider(providers...)
	adrProvider.SetPrinter(out)

	return adrProvider
}
