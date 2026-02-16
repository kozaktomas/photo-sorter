package latex

import "testing"

func TestValidatePages_SlotsWithinBounds(t *testing.T) {
	config := DefaultLayoutConfig()
	// Create a page with a slot that extends beyond canvas right edge
	page := TemplatePage{
		PageNumber:    1,
		IsRecto:       true,
		ContentLeftX:  20.0,
		ContentRightX: 285.0,
		CanvasTopY:    196.0,
		CanvasBottomY: 24.0,
		Slots: []TemplateSlot{
			{
				HasPhoto: true,
				ClipX:    270.0,
				ClipY:    24.0,
				ClipW:    20.0, // extends to 290, past 285
				ClipH:    172.0,
			},
		},
	}
	sections := []TemplateSection{{Pages: []TemplatePage{page}}}
	warnings := ValidatePages(sections, config)

	found := false
	for _, w := range warnings {
		if w.SlotIndex == 0 && w.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error warning for slot extending beyond canvas, got none")
	}
}

func TestValidatePages_NoOverlaps(t *testing.T) {
	config := DefaultLayoutConfig()
	// Two slots that overlap
	page := TemplatePage{
		PageNumber:    1,
		IsRecto:       true,
		ContentLeftX:  20.0,
		ContentRightX: 285.0,
		CanvasTopY:    196.0,
		CanvasBottomY: 24.0,
		Slots: []TemplateSlot{
			{
				HasPhoto: true,
				ClipX:    20.0,
				ClipY:    24.0,
				ClipW:    130.0,
				ClipH:    172.0,
			},
			{
				HasPhoto: true,
				ClipX:    100.0, // overlaps with first slot (20+130=150 > 100)
				ClipY:    24.0,
				ClipW:    130.0,
				ClipH:    172.0,
			},
		},
	}
	sections := []TemplateSection{{Pages: []TemplatePage{page}}}
	warnings := ValidatePages(sections, config)

	found := false
	for _, w := range warnings {
		if w.Message == "slot 0 overlaps with slot 1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected overlap warning between slot 0 and slot 1, got none")
	}
}

func TestValidatePages_ValidPage(t *testing.T) {
	config := DefaultLayoutConfig()
	// Correctly placed non-overlapping slots within canvas
	page := TemplatePage{
		PageNumber:    1,
		IsRecto:       true,
		ContentLeftX:  20.0,
		ContentRightX: 285.0,
		CanvasTopY:    196.0,
		CanvasBottomY: 24.0,
		Slots: []TemplateSlot{
			{
				HasPhoto: true,
				ClipX:    20.0,
				ClipY:    24.0,
				ClipW:    130.5,
				ClipH:    172.0,
			},
			{
				HasPhoto: true,
				ClipX:    154.5,
				ClipY:    24.0,
				ClipW:    130.5,
				ClipH:    172.0,
			},
		},
	}
	sections := []TemplateSection{{Pages: []TemplatePage{page}}}
	warnings := ValidatePages(sections, config)

	if len(warnings) > 0 {
		t.Errorf("expected no warnings for valid layout, got %d: %v", len(warnings), warnings)
	}
}

func TestValidatePages_GridAlignment(t *testing.T) {
	config := DefaultLayoutConfig()
	contentLeftX := config.InsideMarginMM // 20.0 for recto

	t.Run("off-grid slot produces warning", func(t *testing.T) {
		page := TemplatePage{
			PageNumber:    1,
			IsRecto:       true,
			ContentLeftX:  contentLeftX,
			ContentRightX: contentLeftX + config.ContentWidth(),
			CanvasTopY:    196.0,
			CanvasBottomY: 24.0,
			Slots: []TemplateSlot{
				{
					HasPhoto: true,
					ClipX:    contentLeftX + 5.0, // 5mm offset doesn't match any column
					ClipY:    24.0,
					ClipW:    100.0,
					ClipH:    172.0,
				},
			},
		}
		sections := []TemplateSection{{Pages: []TemplatePage{page}}}
		warnings := ValidatePages(sections, config)

		found := false
		for _, w := range warnings {
			if w.SlotIndex == 0 && w.Severity == "warning" && w.Message != "" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected grid alignment warning for off-grid slot, got none")
		}
	})

	t.Run("on-grid slot produces no warning", func(t *testing.T) {
		page := TemplatePage{
			PageNumber:    1,
			IsRecto:       true,
			ContentLeftX:  contentLeftX,
			ContentRightX: contentLeftX + config.ContentWidth(),
			CanvasTopY:    196.0,
			CanvasBottomY: 24.0,
			Slots: []TemplateSlot{
				{
					HasPhoto: true,
					ClipX:    contentLeftX + config.ColOffset(0), // col 0 = 0.0 offset
					ClipY:    24.0,
					ClipW:    config.ColSpanWidth(6),
					ClipH:    172.0,
				},
				{
					HasPhoto: true,
					ClipX:    contentLeftX + config.ColOffset(6), // col 6
					ClipY:    24.0,
					ClipW:    config.ColSpanWidth(6),
					ClipH:    172.0,
				},
			},
		}
		sections := []TemplateSection{{Pages: []TemplatePage{page}}}
		warnings := ValidatePages(sections, config)

		for _, w := range warnings {
			if w.Message != "" && w.Severity == "warning" {
				// Allow gutter-safe warnings but not grid alignment warnings
				if w.Message[:5] == "slot " {
					t.Errorf("unexpected grid alignment warning: %s", w.Message)
				}
			}
		}
	})
}

func TestValidatePages_GutterSafeMarker(t *testing.T) {
	config := DefaultLayoutConfig()
	// Recto page with marker placed near the binding (left) edge — should warn
	page := TemplatePage{
		PageNumber:    1,
		IsRecto:       true,
		ContentLeftX:  20.0,
		ContentRightX: 285.0,
		CanvasTopY:    196.0,
		CanvasBottomY: 24.0,
		Slots: []TemplateSlot{
			{
				HasPhoto:             true,
				ClipX:                20.0,
				ClipY:                24.0,
				ClipW:                130.5,
				ClipH:                172.0,
				CaptionMarker:        1,
				CaptionMarkerX:       22.0, // 2mm from binding edge (20.0), well within 8mm gutter
				CaptionMarkerY:       188.0,
				CaptionMarkerCenterX: 24.0,
				CaptionMarkerCenterY: 190.0,
			},
		},
	}
	sections := []TemplateSection{{Pages: []TemplatePage{page}}}
	warnings := ValidatePages(sections, config)

	found := false
	for _, w := range warnings {
		if w.SlotIndex == 0 && w.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected gutter-safe warning for marker near binding edge, got none")
	}
}
