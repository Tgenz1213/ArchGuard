package config

import "testing"

func TestLoadConfig_Temperature(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want *float64
	}{
		{"unset", "llm:\n  provider: ollama\n", nil},
		{"zero", "llm:\n  temperature: 0.0\n", new(0.0)},
		{"explicit", "llm:\n  temperature: 0.4\n", new(0.4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadFromYAML(t, tt.yaml)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}

			got := cfg.LLM.Temperature
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("Temperature = %v, want %v", got, tt.want)
			}
		})
	}
}
