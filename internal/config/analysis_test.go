package config

import "testing"

func TestAnalysis_RelevantADRLimit(t *testing.T) {
	tests := []struct {
		name string
		max  int
		want int
	}{
		{"unset", 0, 3},
		{"negative", -4, 3},
		{"explicit", 7, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Analysis{MaxRelevantADRs: tt.max}).RelevantADRLimit(); got != tt.want {
				t.Errorf("RelevantADRLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}
