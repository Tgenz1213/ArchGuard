package analysis

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"exact match", "go.sum", "go.sum", true},
		{"exact no match", "go.sum", "go.mod", false},
		{"single star same segment", "*.go", "main.go", true},
		{"single star does not cross segment", "*.go", "internal/main.go", false},
		{"double star suffix matches direct child", "vendor/**", "vendor/foo.go", true},
		{"double star suffix matches nested", "vendor/**", "vendor/pkg/sub/foo.go", true},
		{"double star prefix matches top-level file", "**/*_test.go", "foo_test.go", true},
		{"double star prefix matches nested file (regression)", "**/*_test.go", "internal/analysis/glob_test.go", true},
		{"double star prefix does not match non-test file", "**/*_test.go", "internal/analysis/glob.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchGlob(tt.pattern, tt.path); got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}
