package database

import "testing"

func TestPageFormatSlotCount(t *testing.T) {
	tests := []struct {
		format string
		want   int
	}{
		{"4_landscape", 4},
		{"2l_1p", 3},
		{"1p_2l", 3},
		{"2_portrait", 2},
		{"1_fullscreen", 1},
		{"unknown", 0},
		{"", 0},
		{"4_LANDSCAPE", 0},
		{"2L_1P", 0},
	}

	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			got := PageFormatSlotCount(tc.format)
			if got != tc.want {
				t.Errorf("PageFormatSlotCount(%q) = %d, want %d", tc.format, got, tc.want)
			}
		})
	}
}
