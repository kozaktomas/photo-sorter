package latex

import (
	"fmt"
	"math"
)

// ValidationWarning describes a layout issue found during validation.
type ValidationWarning struct {
	PageNumber int
	SlotIndex  int
	Message    string
	Severity   string // "error" or "warning"
}

// ValidatePages checks all pages for layout integrity issues.
func ValidatePages(sections []TemplateSection, config LayoutConfig) []ValidationWarning {
	totalPages := 0
	for _, sec := range sections {
		totalPages += len(sec.Pages)
	}
	warnings := make([]ValidationWarning, 0, totalPages)
	for _, sec := range sections {
		for _, page := range sec.Pages {
			warnings = append(warnings, validatePage(page, config)...)
		}
	}
	return warnings
}

func validatePage(page TemplatePage, config LayoutConfig) []ValidationWarning {
	var warnings []ValidationWarning

	for i, slot := range page.Slots {
		if !slot.HasPhoto && !slot.HasText {
			continue
		}
		warnings = append(warnings, validateSlotBounds(page, i, slot)...)
		warnings = append(warnings, validateGutterMarker(page, config, i, slot)...)
	}

	warnings = append(warnings, validateGridAlignment(page, config)...)
	warnings = append(warnings, validateNoOverlaps(page)...)

	const eps = 0.01
	if page.HasCaptions && page.CaptionBlockY < config.BottomMarginMM-eps {
		warnings = append(warnings, ValidationWarning{
			PageNumber: page.PageNumber,
			SlotIndex:  -1,
			Message: fmt.Sprintf(
				"caption block Y (%.2f) extends below bottom margin (%.2f)",
				page.CaptionBlockY, config.BottomMarginMM,
			),
			Severity: "warning",
		})
	}

	return warnings
}

// validateSlotBounds checks that a slot's clip rect is within the canvas bounds.
func validateSlotBounds(page TemplatePage, slotIdx int, slot TemplateSlot) []ValidationWarning {
	var warnings []ValidationWarning
	const eps = 0.01

	if slot.ClipX < page.ContentLeftX-eps {
		warnings = append(warnings, ValidationWarning{
			PageNumber: page.PageNumber,
			SlotIndex:  slotIdx,
			Message:    fmt.Sprintf("clip X (%.2f) extends past content left edge (%.2f)", slot.ClipX, page.ContentLeftX),
			Severity:   "error",
		})
	}
	if slot.ClipX+slot.ClipW > page.ContentRightX+eps {
		warnings = append(warnings, ValidationWarning{
			PageNumber: page.PageNumber,
			SlotIndex:  slotIdx,
			Message: fmt.Sprintf(
				"clip right edge (%.2f) extends past content right edge (%.2f)",
				slot.ClipX+slot.ClipW, page.ContentRightX,
			),
			Severity: "error",
		})
	}
	if slot.ClipY < page.CanvasBottomY-eps {
		warnings = append(warnings, ValidationWarning{
			PageNumber: page.PageNumber,
			SlotIndex:  slotIdx,
			Message:    fmt.Sprintf("clip bottom (%.2f) extends below canvas bottom (%.2f)", slot.ClipY, page.CanvasBottomY),
			Severity:   "error",
		})
	}
	if slot.ClipY+slot.ClipH > page.CanvasTopY+eps {
		warnings = append(warnings, ValidationWarning{
			PageNumber: page.PageNumber,
			SlotIndex:  slotIdx,
			Message:    fmt.Sprintf("clip top (%.2f) extends above canvas top (%.2f)", slot.ClipY+slot.ClipH, page.CanvasTopY),
			Severity:   "error",
		})
	}
	return warnings
}

// validateGutterMarker checks that caption markers don't fall in the gutter-safe zone.
func validateGutterMarker(page TemplatePage, config LayoutConfig, slotIdx int, slot TemplateSlot) []ValidationWarning {
	if slot.CaptionMarker <= 0 {
		return nil
	}
	const eps = 0.01
	var insideEdgeX float64
	if page.IsRecto {
		insideEdgeX = page.ContentLeftX
	} else {
		insideEdgeX = page.ContentRightX
	}
	markerSize := config.BaselineUnitMM
	var inGutter bool
	if page.IsRecto {
		markerLeftEdge := slot.CaptionMarkerX
		inGutter = markerLeftEdge < insideEdgeX+config.GutterSafeMM+eps && markerLeftEdge+markerSize > insideEdgeX-eps
	} else {
		markerRightEdge := slot.CaptionMarkerX + markerSize
		inGutter = markerRightEdge > insideEdgeX-config.GutterSafeMM-eps && slot.CaptionMarkerX < insideEdgeX+eps
	}
	if inGutter {
		return []ValidationWarning{{
			PageNumber: page.PageNumber,
			SlotIndex:  slotIdx,
			Message: fmt.Sprintf(
				"caption marker at X=%.2f falls within gutter-safe zone (%.2fmm from binding edge)",
				slot.CaptionMarkerX, config.GutterSafeMM,
			),
			Severity: "warning",
		}}
	}
	return nil
}

// validateNoOverlaps checks all pairs of filled slots for overlapping clip rects.
func validateNoOverlaps(page TemplatePage) []ValidationWarning {
	var warnings []ValidationWarning
	const eps = 0.01

	for i := range len(page.Slots) {
		si := page.Slots[i]
		if !si.HasPhoto && !si.HasText {
			continue
		}
		for j := i + 1; j < len(page.Slots); j++ {
			sj := page.Slots[j]
			if !sj.HasPhoto && !sj.HasText {
				continue
			}
			if rectsOverlap(si.ClipX, si.ClipY, si.ClipW, si.ClipH, sj.ClipX, sj.ClipY, sj.ClipW, sj.ClipH, eps) {
				warnings = append(warnings, ValidationWarning{
					PageNumber: page.PageNumber,
					SlotIndex:  i,
					Message:    fmt.Sprintf("slot %d overlaps with slot %d", i, j),
					Severity:   "error",
				})
			}
		}
	}
	return warnings
}

// validateGridAlignment checks that each slot's X offset aligns with a column edge.
func validateGridAlignment(page TemplatePage, config LayoutConfig) []ValidationWarning {
	var warnings []ValidationWarning
	const eps = 0.01

	// Build set of valid column offsets (relative to content left).
	colOffsets := make([]float64, config.GridColumns)
	for c := range config.GridColumns {
		colOffsets[c] = config.ColOffset(c)
	}

	for i, slot := range page.Slots {
		if !slot.HasPhoto && !slot.HasText {
			continue
		}

		// For archival slots, the border sits on the grid; clip is inset.
		slotX := slot.ClipX - page.ContentLeftX
		if slot.IsArchival {
			slotX = slot.BorderX - page.ContentLeftX
		}

		matched := false
		for _, off := range colOffsets {
			if math.Abs(slotX-off) < eps {
				matched = true
				break
			}
		}
		if !matched {
			warnings = append(warnings, ValidationWarning{
				PageNumber: page.PageNumber,
				SlotIndex:  i,
				Message:    fmt.Sprintf("slot X offset %.2f does not align with any column edge", slotX),
				Severity:   "warning",
			})
		}
	}
	return warnings
}

// rectsOverlap checks if two axis-aligned rectangles overlap with tolerance.
func rectsOverlap(x1, y1, w1, h1, x2, y2, w2, h2, eps float64) bool {
	if x1+w1 <= x2+eps || x2+w2 <= x1+eps {
		return false
	}
	if y1+h1 <= y2+eps || y2+h2 <= y1+eps {
		return false
	}
	return true
}
