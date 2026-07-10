package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalProvider fetches ADRs from the local filesystem.
type LocalProvider struct {
	dirPath          string
	acceptedStatuses []string
}

// NewLocalProvider creates a new LocalProvider.
func NewLocalProvider(dirPath string, acceptedStatuses []string) *LocalProvider {
	return &LocalProvider{
		dirPath:          dirPath,
		acceptedStatuses: acceptedStatuses,
	}
}

// GetADRs walks the directory tree and returns ADRs matching accepted statuses.
func (p *LocalProvider) GetADRs(ctx context.Context) ([]ADR, error) {
	var validADRs []ADR

	err := filepath.Walk(p.dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			adr, err := ParseADR(path, p.dirPath)
			if err != nil {
				fmt.Printf("Warning: skipping %s: %v\n", path, err)
				return nil
			}

			// Filter by status
			accept := false
			for _, status := range p.acceptedStatuses {
				if status == "*" || strings.EqualFold(strings.TrimSpace(adr.Status), strings.TrimSpace(status)) {
					accept = true
					break
				}
			}
			if accept {
				validADRs = append(validADRs, *adr)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return validADRs, nil
}
