package config

import "testing"

func TestLoadConfig_RulesHeading(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unset", "analysis:\n  adr_path: docs/arch\n", ""},
		{"set", "analysis:\n  rules_heading: Detection Rules\n", "Detection Rules"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadFromYAML(t, tt.yaml)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}

			if cfg.Analysis.RulesHeading != tt.want {
				t.Errorf("RulesHeading = %q, want %q", cfg.Analysis.RulesHeading, tt.want)
			}
		})
	}
}

func TestAnalysis_RelevantADRLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"unset", 0, 3},
		{"negative", -4, 3},
		{"explicit", 7, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Analysis{MaxRelevantADRs: tt.limit}).RelevantADRLimit(); got != tt.want {
				t.Errorf("RelevantADRLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}
