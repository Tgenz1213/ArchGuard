package index

import "testing"

func boolPtr(b bool) *bool {
	return &b
}

func float64Ptr(f float64) *float64 {
	return &f
}

func TestPgStore_ReindexEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PgStore{hnsw: HNSWOptions{Enabled: tt.enabled}}
			if got := s.reindexEnabled(); got != tt.want {
				t.Errorf("reindexEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPgStore_ReindexThreshold(t *testing.T) {
	tests := []struct {
		name      string
		threshold *float64
		want      float64
	}{
		{"nil defaults to 0.20", nil, 0.20},
		{"explicit zero is respected, not defaulted", float64Ptr(0), 0},
		{"negative value respected", float64Ptr(-1), -1},
		{"custom value respected", float64Ptr(0.5), 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PgStore{hnsw: HNSWOptions{Threshold: tt.threshold}}
			if got := s.reindexThreshold(); got != tt.want {
				t.Errorf("reindexThreshold() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPgStore_ReindexConcurrently(t *testing.T) {
	tests := []struct {
		name         string
		concurrently *bool
		want         bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PgStore{hnsw: HNSWOptions{Concurrently: tt.concurrently}}
			if got := s.reindexConcurrently(); got != tt.want {
				t.Errorf("reindexConcurrently() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPgStore_ReindexStatement(t *testing.T) {
	tests := []struct {
		name         string
		concurrently *bool
		want         string
	}{
		{"nil defaults to CONCURRENTLY", nil, "REINDEX INDEX CONCURRENTLY archguard_adrs_embedding_idx"},
		{"explicit true uses CONCURRENTLY", boolPtr(true), "REINDEX INDEX CONCURRENTLY archguard_adrs_embedding_idx"},
		{"explicit false uses blocking form", boolPtr(false), "REINDEX INDEX archguard_adrs_embedding_idx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PgStore{hnsw: HNSWOptions{Concurrently: tt.concurrently}}
			if got := s.reindexStatement(); got != tt.want {
				t.Errorf("reindexStatement() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPgStore_IterativeScanConfigured(t *testing.T) {
	tests := []struct {
		name          string
		iterativeScan *bool
		want          bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := HNSWOptions{IterativeScan: tt.iterativeScan}
			if got := o.iterativeScanConfigured(); got != tt.want {
				t.Errorf("iterativeScanConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIterativeScanSupportedVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"0.8.0 supported", "0.8.0", true},
		{"0.8.5 supported", "0.8.5", true},
		{"1.0.0 supported", "1.0.0", true},
		{"pre-0.8", "0.7.4", false},
		{"0.7.0 unsupported", "0.7.0", false},
		{"malformed string", "abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IterativeScanSupportedVersion(tt.version); got != tt.want {
				t.Errorf("IterativeScanSupportedVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
