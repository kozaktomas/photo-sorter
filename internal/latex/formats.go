package latex

// Page dimensions in mm (A4 landscape).
const (
	PageW = 297.0
	PageH = 210.0
)

// SectionHeadingHeightMM is the vertical space reserved for an inline section.
// heading on the first content page of a section (rule + title + gap).
const SectionHeadingHeightMM = 14.0

// LayoutConfig holds the 12-column grid and 3-zone page layout configuration.
type LayoutConfig struct {
	InsideMarginMM  float64 // binding side (20mm)
	OutsideMarginMM float64 // away from binding (12mm)
	TopMarginMM     float64 // 10mm
	BottomMarginMM  float64 // 16mm
	GridColumns     int     // 12
	ColumnGutterMM  float64 // 4mm between columns
	RowGapMM        float64 // 4mm between rows
	HeaderHeightMM  float64 // 4mm running header zone
	CanvasHeightMM  float64 // 172mm photo/text zone
	FooterHeightMM  float64 // 8mm captions + folio zone
	ArchivalInsetMM float64 // 3mm mat inset for archival photos
	GutterSafeMM    float64 // 8mm inset from inside content edge
	BaselineUnitMM  float64 // 4mm vertical rhythm unit
}

// DefaultLayoutConfig returns the print-ready layout configuration.
func DefaultLayoutConfig() LayoutConfig {
	return LayoutConfig{
		InsideMarginMM:  20.0,
		OutsideMarginMM: 12.0,
		TopMarginMM:     10.0,
		BottomMarginMM:  16.0,
		GridColumns:     12,
		ColumnGutterMM:  4.0,
		RowGapMM:        4.0,
		HeaderHeightMM:  4.0,
		CanvasHeightMM:  172.0,
		FooterHeightMM:  8.0,
		ArchivalInsetMM: 3.0,
		GutterSafeMM:    8.0,
		BaselineUnitMM:  4.0,
	}
}

// ContentWidth returns the usable horizontal space (same for all pages).
// 297 - 20 - 12 = 265mm.
func (c LayoutConfig) ContentWidth() float64 {
	return PageW - c.InsideMarginMM - c.OutsideMarginMM
}

// ColumnWidth returns the width of a single grid column.
// (265 - 11*4) / 12 = (265 - 44) / 12 = 221 / 12 = 18.42mm.
func (c LayoutConfig) ColumnWidth() float64 {
	return (c.ContentWidth() - float64(c.GridColumns-1)*c.ColumnGutterMM) / float64(c.GridColumns)
}

// ColSpanWidth returns the width of n adjacent columns including internal gutters.
// n columns + (n-1) gutters.
func (c LayoutConfig) ColSpanWidth(n int) float64 {
	return float64(n)*c.ColumnWidth() + float64(n-1)*c.ColumnGutterMM
}

// ColOffset returns the X offset of a 0-indexed column from the content left edge.
func (c LayoutConfig) ColOffset(col int) float64 {
	return float64(col) * (c.ColumnWidth() + c.ColumnGutterMM)
}

// HalfCanvasHeight returns the height of a half-row: (canvas - rowGap) / 2.
func (c LayoutConfig) HalfCanvasHeight() float64 {
	return (c.CanvasHeightMM - c.RowGapMM) / 2.0
}

// SlotRect defines a slot position in the canvas zone (mm, origin at top-left of canvas).
type SlotRect struct {
	X, Y, W, H float64
}

// FormatSlotsGrid returns slot rectangles for a page format using the 12-column grid.
// Coordinates are relative to the canvas zone origin (top-left of canvas area).
func FormatSlotsGrid(format string, config LayoutConfig) []SlotRect {
	cw := config.ContentWidth()
	ch := config.CanvasHeightMM
	halfH := config.HalfCanvasHeight()
	halfW := config.ColSpanWidth(6)
	gap := config.ColumnGutterMM
	rowGap := config.RowGapMM

	switch format {
	case "1_fullscreen":
		// Slot 0: cols 1-12, full canvas.
		return []SlotRect{
			{0, 0, cw, ch},
		}

	case "2_portrait":
		// Slot 0: cols 1-6, full canvas.
		// Slot 1: cols 7-12, full canvas.
		return []SlotRect{
			{0, 0, halfW, ch},
			{halfW + gap, 0, halfW, ch},
		}

	case "4_landscape":
		// Slot 0: cols 1-6, top row  |  Slot 1: cols 7-12, top row.
		// Slot 2: cols 1-6, bottom   |  Slot 3: cols 7-12, bottom.
		return []SlotRect{
			{0, 0, halfW, halfH},
			{halfW + gap, 0, halfW, halfH},
			{0, halfH + rowGap, halfW, halfH},
			{halfW + gap, halfH + rowGap, halfW, halfH},
		}

	case "2l_1p":
		// Slots 0,1 (landscape): cols 1-8 stacked.
		// Slot 2 (portrait): cols 9-12.
		leftW := config.ColSpanWidth(8)
		rightW := config.ColSpanWidth(4)
		rightX := config.ColOffset(8)
		return []SlotRect{
			{0, 0, leftW, halfH},
			{0, halfH + rowGap, leftW, halfH},
			{rightX, 0, rightW, ch},
		}

	case "1p_2l":
		// Slot 0 (portrait): cols 1-4.
		// Slots 1,2 (landscape): cols 5-12 stacked.
		leftW := config.ColSpanWidth(4)
		rightW := config.ColSpanWidth(8)
		rightX := config.ColOffset(4)
		return []SlotRect{
			{0, 0, leftW, ch},
			{rightX, 0, rightW, halfH},
			{rightX, halfH + rowGap, rightW, halfH},
		}

	default:
		return nil
	}
}

// FormatSlotsGridWithSplit returns slot rectangles using a custom split position.
// When splitPosition is nil or the format is 1_fullscreen, it delegates to FormatSlotsGrid.
func FormatSlotsGridWithSplit(format string, config LayoutConfig, splitPosition *float64) []SlotRect {
	if splitPosition == nil || format == "1_fullscreen" {
		return FormatSlotsGrid(format, config)
	}

	split := *splitPosition
	cw := config.ContentWidth()
	ch := config.CanvasHeightMM
	halfH := config.HalfCanvasHeight()
	gap := config.ColumnGutterMM
	rowGap := config.RowGapMM

	// Available width minus the gutter between left and right columns.
	availW := cw - gap
	leftW := availW * split
	rightW := availW * (1 - split)
	rightX := leftW + gap

	switch format {
	case "2_portrait":
		return []SlotRect{
			{0, 0, leftW, ch},
			{rightX, 0, rightW, ch},
		}

	case "4_landscape":
		return []SlotRect{
			{0, 0, leftW, halfH},
			{rightX, 0, rightW, halfH},
			{0, halfH + rowGap, leftW, halfH},
			{rightX, halfH + rowGap, rightW, halfH},
		}

	case "2l_1p":
		return []SlotRect{
			{0, 0, leftW, halfH},
			{0, halfH + rowGap, leftW, halfH},
			{rightX, 0, rightW, ch},
		}

	case "1p_2l":
		return []SlotRect{
			{0, 0, leftW, ch},
			{rightX, 0, rightW, halfH},
			{rightX, halfH + rowGap, rightW, halfH},
		}

	default:
		return FormatSlotsGrid(format, config)
	}
}
