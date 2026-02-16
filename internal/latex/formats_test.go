package latex

import (
	"fmt"
	"math"
	"testing"
)

func TestDefaultLayoutConfig(t *testing.T) {
	cfg := DefaultLayoutConfig()
	if cfg.InsideMarginMM <= 0 || cfg.OutsideMarginMM <= 0 {
		t.Error("margins should be positive")
	}
	if cfg.GridColumns != 12 {
		t.Errorf("expected 12 columns, got %d", cfg.GridColumns)
	}
}

func TestContentWidth(t *testing.T) {
	cfg := DefaultLayoutConfig()
	// 297 - 20 - 12 = 265
	expected := 265.0
	got := cfg.ContentWidth()
	if math.Abs(got-expected) > 0.01 {
		t.Errorf("ContentWidth: expected %.2f, got %.2f", expected, got)
	}
}

func TestColumnWidth(t *testing.T) {
	cfg := DefaultLayoutConfig()
	// (265 - 11*4) / 12 = (265 - 44) / 12 = 221 / 12 = 18.42
	expected := 18.42
	got := cfg.ColumnWidth()
	if math.Abs(got-expected) > 0.01 {
		t.Errorf("ColumnWidth: expected %.2f, got %.2f", expected, got)
	}
}

func TestColSpanWidth(t *testing.T) {
	cfg := DefaultLayoutConfig()
	// 6 columns: 6*18.42 + 5*4 = 110.5 + 20 = 130.5
	expected6 := 130.5
	got6 := cfg.ColSpanWidth(6)
	if math.Abs(got6-expected6) > 0.01 {
		t.Errorf("ColSpanWidth(6): expected %.2f, got %.2f", expected6, got6)
	}

	// 12 columns: full width = 257
	got12 := cfg.ColSpanWidth(12)
	if math.Abs(got12-cfg.ContentWidth()) > 0.01 {
		t.Errorf("ColSpanWidth(12): expected %.2f (full width), got %.2f", cfg.ContentWidth(), got12)
	}

	// 4 columns: 4*18.42 + 3*4 = 73.67 + 12 = 85.67
	expected4 := 85.67
	got4 := cfg.ColSpanWidth(4)
	if math.Abs(got4-expected4) > 0.01 {
		t.Errorf("ColSpanWidth(4): expected %.2f, got %.2f", expected4, got4)
	}

	// 8 columns: 8*18.42 + 7*4 = 147.33 + 28 = 175.33
	expected8 := 175.33
	got8 := cfg.ColSpanWidth(8)
	if math.Abs(got8-expected8) > 0.01 {
		t.Errorf("ColSpanWidth(8): expected %.2f, got %.2f", expected8, got8)
	}
}

func TestZonesHeight(t *testing.T) {
	cfg := DefaultLayoutConfig()
	// Top margin(10) + header(4) + canvas(172) + footer(8) + bottom margin(16) = 210
	total := cfg.TopMarginMM + cfg.HeaderHeightMM + cfg.CanvasHeightMM + cfg.FooterHeightMM + cfg.BottomMarginMM
	if math.Abs(total-PageH) > 0.01 {
		t.Errorf("zones should sum to page height: %.2f + %.2f + %.2f + %.2f + %.2f = %.2f, expected %.2f",
			cfg.TopMarginMM, cfg.HeaderHeightMM, cfg.CanvasHeightMM, cfg.FooterHeightMM, cfg.BottomMarginMM, total, PageH)
	}
}

func TestFormatSlotsGrid_SlotCounts(t *testing.T) {
	cfg := DefaultLayoutConfig()
	tests := []struct {
		format string
		count  int
	}{
		{"1_fullscreen", 1},
		{"2_portrait", 2},
		{"4_landscape", 4},
		{"2l_1p", 3},
		{"1p_2l", 3},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			slots := FormatSlotsGrid(tt.format, cfg)
			if len(slots) != tt.count {
				t.Errorf("expected %d slots, got %d", tt.count, len(slots))
			}
		})
	}
}

func TestFormatSlotsGrid_SlotsWithinBounds(t *testing.T) {
	cfg := DefaultLayoutConfig()
	formats := []string{"1_fullscreen", "2_portrait", "4_landscape", "2l_1p", "1p_2l"}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			slots := FormatSlotsGrid(format, cfg)
			for i, s := range slots {
				if s.X < -0.01 || s.Y < -0.01 {
					t.Errorf("slot %d: negative position (%.2f, %.2f)", i, s.X, s.Y)
				}
				if s.X+s.W > cfg.ContentWidth()+0.01 {
					t.Errorf("slot %d: X+W (%.2f) exceeds content width (%.2f)", i, s.X+s.W, cfg.ContentWidth())
				}
				if s.Y+s.H > cfg.CanvasHeightMM+0.01 {
					t.Errorf("slot %d: Y+H (%.2f) exceeds canvas height (%.2f)", i, s.Y+s.H, cfg.CanvasHeightMM)
				}
			}
		})
	}
}

func TestFormatSlotsGrid_ColumnAlignment(t *testing.T) {
	cfg := DefaultLayoutConfig()
	const eps = 0.01

	// 1_fullscreen: slot starts at col 0, width = full
	fs := FormatSlotsGrid("1_fullscreen", cfg)
	if math.Abs(fs[0].X) > eps {
		t.Errorf("1_fullscreen slot X should be 0, got %.2f", fs[0].X)
	}
	if math.Abs(fs[0].W-cfg.ContentWidth()) > eps {
		t.Errorf("1_fullscreen slot W should be content width, got %.2f", fs[0].W)
	}

	// 2_portrait: each slot is 6 columns wide
	pp := FormatSlotsGrid("2_portrait", cfg)
	halfW := cfg.ColSpanWidth(6)
	if math.Abs(pp[0].W-halfW) > eps {
		t.Errorf("2_portrait slot 0 width: expected %.2f, got %.2f", halfW, pp[0].W)
	}
	if math.Abs(pp[1].W-halfW) > eps {
		t.Errorf("2_portrait slot 1 width: expected %.2f, got %.2f", halfW, pp[1].W)
	}

	// 2l_1p: slots 0,1 are 8 cols, slot 2 is 4 cols
	lp := FormatSlotsGrid("2l_1p", cfg)
	w8 := cfg.ColSpanWidth(8)
	w4 := cfg.ColSpanWidth(4)
	if math.Abs(lp[0].W-w8) > eps {
		t.Errorf("2l_1p slot 0 width: expected %.2f, got %.2f", w8, lp[0].W)
	}
	if math.Abs(lp[2].W-w4) > eps {
		t.Errorf("2l_1p slot 2 width: expected %.2f, got %.2f", w4, lp[2].W)
	}

	// 1p_2l: slot 0 is 4 cols, slots 1,2 are 8 cols
	pl := FormatSlotsGrid("1p_2l", cfg)
	if math.Abs(pl[0].W-w4) > eps {
		t.Errorf("1p_2l slot 0 width: expected %.2f, got %.2f", w4, pl[0].W)
	}
	if math.Abs(pl[1].W-w8) > eps {
		t.Errorf("1p_2l slot 1 width: expected %.2f, got %.2f", w8, pl[1].W)
	}
}

func TestFormatSlotsGrid_InvalidFormat(t *testing.T) {
	cfg := DefaultLayoutConfig()
	result := FormatSlotsGrid("nonexistent", cfg)
	if result != nil {
		t.Errorf("expected nil for invalid format, got %+v", result)
	}
}

func TestGutterSafeConstant(t *testing.T) {
	cfg := DefaultLayoutConfig()
	if cfg.GutterSafeMM != 8.0 {
		t.Errorf("GutterSafeMM: expected 8.0, got %.2f", cfg.GutterSafeMM)
	}
}

func TestBaselineUnitConstant(t *testing.T) {
	cfg := DefaultLayoutConfig()
	if cfg.BaselineUnitMM != 4.0 {
		t.Errorf("BaselineUnitMM: expected 4.0, got %.2f", cfg.BaselineUnitMM)
	}
}

func TestHalfCanvasHeight(t *testing.T) {
	cfg := DefaultLayoutConfig()
	// (172 - 4) / 2 = 84
	expected := 84.0
	got := cfg.HalfCanvasHeight()
	if math.Abs(got-expected) > 0.01 {
		t.Errorf("HalfCanvasHeight: expected %.2f, got %.2f", expected, got)
	}
}

// --- ColOffset ---

func TestColOffset(t *testing.T) {
	cfg := DefaultLayoutConfig()
	colW := cfg.ColumnWidth()
	gutter := cfg.ColumnGutterMM
	const eps = 0.01

	tests := []struct {
		col      int
		expected float64
	}{
		{0, 0},
		{1, colW + gutter},
		{6, 6 * (colW + gutter)},
		{11, 11 * (colW + gutter)},
	}
	for _, tt := range tests {
		got := cfg.ColOffset(tt.col)
		if math.Abs(got-tt.expected) > eps {
			t.Errorf("ColOffset(%d): expected %.4f, got %.4f", tt.col, tt.expected, got)
		}
	}
}

// --- FormatSlotsGridWithSplit ---

func TestFormatSlotsGridWithSplit_NilFallback(t *testing.T) {
	cfg := DefaultLayoutConfig()
	formats := []string{"1_fullscreen", "2_portrait", "4_landscape", "2l_1p", "1p_2l"}
	const eps = 0.01

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			base := FormatSlotsGrid(format, cfg)
			withNil := FormatSlotsGridWithSplit(format, cfg, nil)
			if len(base) != len(withNil) {
				t.Fatalf("slot count mismatch: base=%d, withNil=%d", len(base), len(withNil))
			}
			for i := range base {
				if math.Abs(base[i].X-withNil[i].X) > eps ||
					math.Abs(base[i].Y-withNil[i].Y) > eps ||
					math.Abs(base[i].W-withNil[i].W) > eps ||
					math.Abs(base[i].H-withNil[i].H) > eps {
					t.Errorf("slot %d differs: base=%+v, withNil=%+v", i, base[i], withNil[i])
				}
			}
		})
	}
}

func TestFormatSlotsGridWithSplit_CustomSplit(t *testing.T) {
	cfg := DefaultLayoutConfig()
	const eps = 0.01
	cw := cfg.ContentWidth()
	gap := cfg.ColumnGutterMM

	splits := []float64{0.2, 0.5, 0.8}

	for _, format := range []string{"2l_1p", "1p_2l", "2_portrait", "4_landscape"} {
		for _, split := range splits {
			name := format + "_" + formatFloat(split)
			t.Run(name, func(t *testing.T) {
				sp := split
				slots := FormatSlotsGridWithSplit(format, cfg, &sp)
				if len(slots) == 0 {
					t.Fatal("expected non-empty slots")
				}

				// Verify total width coverage: leftW + gap + rightW = cw
				availW := cw - gap
				leftW := availW * split
				rightW := availW * (1 - split)
				rightX := leftW + gap

				// Check first slot is at X=0 and has leftW width
				if math.Abs(slots[0].X) > eps {
					t.Errorf("first slot X: expected 0, got %.4f", slots[0].X)
				}
				if math.Abs(slots[0].W-leftW) > eps {
					t.Errorf("first slot W: expected %.4f, got %.4f", leftW, slots[0].W)
				}

				// Find right-side slots and verify position
				for _, s := range slots {
					if s.X > eps { // right-side slot
						if math.Abs(s.X-rightX) > eps {
							t.Errorf("right slot X: expected %.4f, got %.4f", rightX, s.X)
						}
						if math.Abs(s.W-rightW) > eps {
							t.Errorf("right slot W: expected %.4f, got %.4f", rightW, s.W)
						}
					}
				}
			})
		}
	}
}

func TestFormatSlotsGridWithSplit_BoundsCheck(t *testing.T) {
	cfg := DefaultLayoutConfig()
	const eps = 0.01
	cw := cfg.ContentWidth()
	ch := cfg.CanvasHeightMM

	formats := []string{"1_fullscreen", "2_portrait", "4_landscape", "2l_1p", "1p_2l"}
	splits := []float64{0.2, 0.35, 0.5, 0.65, 0.8}

	for _, format := range formats {
		for _, split := range splits {
			name := format + "_" + formatFloat(split)
			t.Run(name, func(t *testing.T) {
				sp := split
				slots := FormatSlotsGridWithSplit(format, cfg, &sp)
				for i, s := range slots {
					if s.X < -eps || s.Y < -eps {
						t.Errorf("slot %d: negative position (%.4f, %.4f)", i, s.X, s.Y)
					}
					if s.X+s.W > cw+eps {
						t.Errorf("slot %d: X+W (%.4f) exceeds content width (%.4f)", i, s.X+s.W, cw)
					}
					if s.Y+s.H > ch+eps {
						t.Errorf("slot %d: Y+H (%.4f) exceeds canvas height (%.4f)", i, s.Y+s.H, ch)
					}
					if s.W <= 0 || s.H <= 0 {
						t.Errorf("slot %d: non-positive dimensions (%.4f x %.4f)", i, s.W, s.H)
					}
				}
			})
		}
	}
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.1f", f)
}
