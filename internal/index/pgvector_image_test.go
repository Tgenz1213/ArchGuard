package index_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tgenz1213/archguard/internal/index"
)

var (
	pgvectorImageRef  = regexp.MustCompile(`pgvector/pgvector:([^\s"']+)`)
	pinnedPgvectorTag = regexp.MustCompile(`^(\d+\.\d+\.\d+)-pg\d+$`)
)

type pgvectorTagSource struct {
	path    string
	content string
}

// checkPgvectorTags enforces the pin that docs/arch/0005 relies on: every
// source names one pgvector/pgvector tag, the same one, of the form
// <pgvector-version>-pg<postgres-major>, at a version with hnsw.iterative_scan.
func checkPgvectorTags(sources []pgvectorTagSource) error {
	paths := make([]string, len(sources))
	tags := make([]string, len(sources))

	for i, source := range sources {
		matches := pgvectorImageRef.FindAllStringSubmatch(source.content, -1)
		if len(matches) != 1 {
			return fmt.Errorf("%s names %d pgvector/pgvector image tags, want exactly 1", source.path, len(matches))
		}

		paths[i] = source.path
		tags[i] = matches[0][1]
	}

	for i := 1; i < len(tags); i++ {
		if tags[i] != tags[0] {
			return fmt.Errorf("%s pins pgvector/pgvector:%s but %s pins pgvector/pgvector:%s; change both to the same tag", paths[0], tags[0], paths[i], tags[i])
		}
	}

	pinned := strings.Join(paths, " and ")

	version := pinnedPgvectorTag.FindStringSubmatch(tags[0])
	if version == nil {
		return fmt.Errorf("%s pin pgvector/pgvector:%s; want a fixed <pgvector-version>-pg<postgres-major> tag such as 0.8.7-pg16", pinned, tags[0])
	}

	if !index.IterativeScanSupportedVersion(version[1]) {
		return fmt.Errorf("%s pin pgvector %s, which lacks hnsw.iterative_scan; use 0.8.0 or later (docs/arch/0005-hnsw-iterative-scan-for-project-filtered-search.md)", pinned, version[1])
	}

	return nil
}

func TestPgvectorImageTag_PinnedAndInSync(t *testing.T) {
	paths := []string{filepath.Join("..", "..", "docker-compose.yml"), "pgvector_integration_test.go"}

	sources := make([]pgvectorTagSource, len(paths))
	for i, path := range paths {
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		sources[i] = pgvectorTagSource{path: path, content: string(content)}
	}

	assert.NoError(t, checkPgvectorTags(sources))
}

func TestCheckPgvectorTags(t *testing.T) {
	tests := []struct {
		name    string
		compose string
		test    string
		wantErr bool
	}{
		{name: "same pinned tag", compose: "pgvector/pgvector:0.8.7-pg16", test: `"pgvector/pgvector:0.8.7-pg16"`},
		{name: "tags differ", compose: "pgvector/pgvector:0.8.7-pg16", test: `"pgvector/pgvector:0.8.8-pg16"`, wantErr: true},
		{name: "postgres major only", compose: "pgvector/pgvector:pg16", test: `"pgvector/pgvector:pg16"`, wantErr: true},
		{name: "latest", compose: "pgvector/pgvector:latest", test: `"pgvector/pgvector:latest"`, wantErr: true},
		{name: "version without iterative scan", compose: "pgvector/pgvector:0.7.4-pg16", test: `"pgvector/pgvector:0.7.4-pg16"`, wantErr: true},
		{name: "no tag in one file", compose: "postgres:16", test: `"pgvector/pgvector:0.8.7-pg16"`, wantErr: true},
		{name: "two tags in one file", compose: "pgvector/pgvector:0.8.7-pg16\npgvector/pgvector:0.8.7-pg17", test: `"pgvector/pgvector:0.8.7-pg16"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPgvectorTags([]pgvectorTagSource{
				{path: "docker-compose.yml", content: tt.compose},
				{path: "pgvector_integration_test.go", content: tt.test},
			})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}
