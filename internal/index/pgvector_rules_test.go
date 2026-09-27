package index

import "testing"

func TestRulesEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b Rules
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil and empty", nil, Rules{}, true},
		{"same statements", statements("A", "B"), statements("A", "B"), true},
		{"different statement", statements("A"), statements("B"), false},
		{"different order", statements("A", "B"), statements("B", "A"), false},
		{"rules added", nil, statements("A"), false},
		{"example added", statements("A"), Rules{{Statement: "A", Violating: []string{"bad()"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rulesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("rulesEqual = %v, want %v", got, tt.want)
			}
		})
	}
}
