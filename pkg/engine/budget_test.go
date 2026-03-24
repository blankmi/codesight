package engine

import "testing"

func TestComputeBudget(t *testing.T) {
	tests := []struct {
		name      string
		flag      string
		depth     int
		wantTotal int
	}{
		{"auto depth 1", "auto", 1, 80},
		{"auto depth 2", "auto", 2, 140}, // medium(120) + 20
		{"auto depth 3", "auto", 3, 160}, // medium(120) + 40
		{"small depth 1", "small", 1, 80},
		{"medium depth 1", "medium", 1, 120},
		{"large depth 1", "large", 1, 160},
		{"large depth 2", "large", 2, 180},     // 160 + 20
		{"large depth 5", "large", 5, 240},     // 160 + 80 = 240 (capped)
		{"empty defaults to auto", "", 1, 80},
		{"hard cap", "large", 10, 240},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := ComputeBudget(tt.flag, tt.depth)
			if b.TotalLines != tt.wantTotal {
				t.Errorf("TotalLines = %d, want %d", b.TotalLines, tt.wantTotal)
			}
			// Verify allocations sum to total.
			sum := b.DefinitionLines + b.ReferenceLines + b.CallerLines + b.MetaLines
			if sum != b.TotalLines {
				t.Errorf("allocations sum = %d, want %d", sum, b.TotalLines)
			}
			// Definition should get the largest share.
			if b.DefinitionLines < b.ReferenceLines {
				t.Errorf("DefinitionLines (%d) < ReferenceLines (%d)", b.DefinitionLines, b.ReferenceLines)
			}
		})
	}
}
