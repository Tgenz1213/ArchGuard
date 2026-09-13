package index

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LocalProvider fetches ADRs from the local filesystem.
type LocalProvider struct {
	dirPath          string
	acceptedStatuses []string
	idPattern        *regexp.Regexp
	writer           io.Writer
}

// NewLocalProvider creates a new LocalProvider.
func NewLocalProvider(dirPath string, acceptedStatuses []string) *LocalProvider {
	return &LocalProvider{
		dirPath:          dirPath,
		acceptedStatuses: acceptedStatuses,
	}
}

// SetIDPattern overrides the default filename-based ADR ID extraction (see
// extractID in adr.go). Passing nil restores the default behavior.
func (p *LocalProvider) SetIDPattern(re *regexp.Regexp) {
	p.idPattern = re
}

// SetWriter routes GetADRs' parse-failure warnings to w instead of the
// default os.Stdout. Passing nil restores the default.
func (p *LocalProvider) SetWriter(w io.Writer) {
	p.writer = w
}

// GetADRs walks the directory tree and returns ADRs matching accepted statuses.
func (p *LocalProvider) GetADRs(ctx context.Context) ([]ADR, FetchStats, error) {
	var validADRs []ADR
	var stats FetchStats

	err := filepath.Walk(p.dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			stats.Discovered++
			adr, err := ParseADR(path, p.dirPath, p.idPattern)
			if err != nil {
				_, _ = fmt.Fprintf(diagWriter(p.writer), "Warning: skipping %s: %v\n", path, err)
				stats.ParseFailed = append(stats.ParseFailed, path)
				return nil
			}

			if isAcceptedStatus(adr.Status, p.acceptedStatuses) {
				validADRs = append(validADRs, *adr)
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
