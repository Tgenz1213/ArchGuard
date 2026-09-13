package analysis

import (
	"reflect"
	"testing"
)

func TestMultiFileProvider_GetFiles(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{
			name:  "no duplicates",
			paths: []string{"a.go", "b.go", "c.go"},
			want:  []string{"a.go", "b.go", "c.go"},
		},
		{
			name:  "duplicate path deduplicated, first occurrence order preserved",
			paths: []string{"a.go", "b.go", "a.go"},
			want:  []string{"a.go", "b.go"},
		},
		{
			name:  "single file",
			paths: []string{"a.go"},
			want:  []string{"a.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &MultiFileProvider{Paths: tt.paths}
			got, err := p.GetFiles()
			if err != nil {
				t.Fatalf("GetFiles() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
