package index

import "testing"

func boolPtr(b bool) *bool {
	return &b
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
			s := &PgStore{reindex: ReindexOptions{Enabled: tt.enabled}}
			if got := s.reindexEnabled(); got != tt.want {
				t.Errorf("reindexEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPgStore_ReindexThreshold(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		want      float64
	}{
		{"zero defaults to 0.20", 0, 0.20},
		{"negative defaults to 0.20", -1, 0.20},
		{"custom value respected", 0.5, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PgStore{reindex: ReindexOptions{Threshold: tt.threshold}}
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
			s := &PgStore{reindex: ReindexOptions{Concurrently: tt.concurrently}}
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
			s := &PgStore{reindex: ReindexOptions{Concurrently: tt.concurrently}}
			if got := s.reindexStatement(); got != tt.want {
				t.Errorf("reindexStatement() = %q, want %q", got, tt.want)
			}
		})
	}
}
