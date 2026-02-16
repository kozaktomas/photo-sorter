package latex

import (
	"math"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

const eps = 0.01

// --- collectPhotoUIDs ---

func TestCollectPhotoUIDs(t *testing.T) {
	t.Run("empty pages", func(t *testing.T) {
		got := collectPhotoUIDs(nil)
		if len(got) != 0 {
			t.Errorf("expected empty map, got %d entries", len(got))
		}
	})

	t.Run("single slot", func(t *testing.T) {
		pages := []database.BookPage{
			{Slots: []database.PageSlot{{PhotoUID: "p1"}}},
		}
		got := collectPhotoUIDs(pages)
		if !got["p1"] {
			t.Error("expected p1 in UID set")
		}
		if len(got) != 1 {
			t.Errorf("expected 1 entry, got %d", len(got))
		}
	})

	t.Run("multiple slots across pages", func(t *testing.T) {
		pages := []database.BookPage{
			{Slots: []database.PageSlot{{PhotoUID: "p1"}, {PhotoUID: "p2"}}},
			{Slots: []database.PageSlot{{PhotoUID: "p3"}}},
		}
		got := collectPhotoUIDs(pages)
		if len(got) != 3 {
			t.Errorf("expected 3 entries, got %d", len(got))
		}
	})

	t.Run("text slots ignored", func(t *testing.T) {
		pages := []database.BookPage{
			{Slots: []database.PageSlot{{TextContent: "some text"}, {PhotoUID: "p1"}}},
		}
		got := collectPhotoUIDs(pages)
		if len(got) != 1 {
			t.Errorf("expected 1 entry (text slots ignored), got %d", len(got))
		}
	})

	t.Run("dedup", func(t *testing.T) {
		pages := []database.BookPage{
			{Slots: []database.PageSlot{{PhotoUID: "p1"}, {PhotoUID: "p1"}}},
			{Slots: []database.PageSlot{{PhotoUID: "p1"}}},
		}
		got := collectPhotoUIDs(pages)
		if len(got) != 1 {
			t.Errorf("expected 1 entry after dedup, got %d", len(got))
		}
	})
}

// --- sortPagesBySectionOrder ---

func TestSortPagesBySectionOrder(t *testing.T) {
	t.Run("single section", func(t *testing.T) {
		pages := []database.BookPage{
			{ID: "p2", SectionID: "s1", SortOrder: 2},
			{ID: "p1", SectionID: "s1", SortOrder: 1},
		}
		sections := []database.BookSection{{ID: "s1"}}
		sortPagesBySectionOrder(pages, sections)
		if pages[0].ID != "p1" || pages[1].ID != "p2" {
			t.Errorf("expected [p1, p2], got [%s, %s]", pages[0].ID, pages[1].ID)
		}
	})

	t.Run("multi-section ordering", func(t *testing.T) {
		pages := []database.BookPage{
			{ID: "p2", SectionID: "s2", SortOrder: 1},
			{ID: "p1", SectionID: "s1", SortOrder: 1},
		}
		sections := []database.BookSection{{ID: "s1"}, {ID: "s2"}}
		sortPagesBySectionOrder(pages, sections)
		if pages[0].ID != "p1" || pages[1].ID != "p2" {
			t.Errorf("expected [p1, p2], got [%s, %s]", pages[0].ID, pages[1].ID)
		}
	})

	t.Run("within-section stable sort", func(t *testing.T) {
		pages := []database.BookPage{
			{ID: "p3", SectionID: "s1", SortOrder: 3},
			{ID: "p1", SectionID: "s1", SortOrder: 1},
			{ID: "p2", SectionID: "s1", SortOrder: 2},
		}
		sections := []database.BookSection{{ID: "s1"}}
		sortPagesBySectionOrder(pages, sections)
		if pages[0].ID != "p1" || pages[1].ID != "p2" || pages[2].ID != "p3" {
			t.Errorf("expected [p1, p2, p3], got [%s, %s, %s]", pages[0].ID, pages[1].ID, pages[2].ID)
		}
	})

	t.Run("empty pages", func(t *testing.T) {
		var pages []database.BookPage
		sections := []database.BookSection{{ID: "s1"}}
		sortPagesBySectionOrder(pages, sections) // should not panic
		if len(pages) != 0 {
			t.Error("expected empty")
		}
	})
}

// --- groupPagesBySection ---

func TestGroupPagesBySection(t *testing.T) {
	t.Run("single section", func(t *testing.T) {
		pages := []database.BookPage{
			{SectionID: "s1"},
			{SectionID: "s1"},
		}
		sections := []database.BookSection{{ID: "s1", Title: "Section 1"}}
		groups := groupPagesBySection(pages, sections)
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		if groups[0].title != "Section 1" {
			t.Errorf("expected title 'Section 1', got '%s'", groups[0].title)
		}
		if len(groups[0].pages) != 2 {
			t.Errorf("expected 2 pages, got %d", len(groups[0].pages))
		}
	})

	t.Run("two sections", func(t *testing.T) {
		pages := []database.BookPage{
			{SectionID: "s1"},
			{SectionID: "s2"},
		}
		sections := []database.BookSection{{ID: "s1", Title: "A"}, {ID: "s2", Title: "B"}}
		groups := groupPagesBySection(pages, sections)
		if len(groups) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(groups))
		}
		if groups[0].title != "A" || groups[1].title != "B" {
			t.Errorf("unexpected titles: %q, %q", groups[0].title, groups[1].title)
		}
	})

	t.Run("alternating sections creates separate groups", func(t *testing.T) {
		pages := []database.BookPage{
			{SectionID: "s1"},
			{SectionID: "s2"},
			{SectionID: "s1"},
		}
		sections := []database.BookSection{{ID: "s1", Title: "A"}, {ID: "s2", Title: "B"}}
		groups := groupPagesBySection(pages, sections)
		if len(groups) != 3 {
			t.Fatalf("expected 3 groups (alternating), got %d", len(groups))
		}
	})

	t.Run("empty", func(t *testing.T) {
		groups := groupPagesBySection(nil, nil)
		if len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(groups))
		}
	})

	t.Run("missing title", func(t *testing.T) {
		pages := []database.BookPage{{SectionID: "s1"}}
		groups := groupPagesBySection(pages, nil) // no sections with titles
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		if groups[0].title != "" {
			t.Errorf("expected empty title, got '%s'", groups[0].title)
		}
	})
}

// --- lookupCaption ---

func TestLookupCaption(t *testing.T) {
	captions := captionMap{
		"s1": {"p1": "Caption for p1"},
	}

	t.Run("found", func(t *testing.T) {
		got := lookupCaption(captions, "s1", "p1")
		if got != "Caption for p1" {
			t.Errorf("expected 'Caption for p1', got '%s'", got)
		}
	})

	t.Run("section not found", func(t *testing.T) {
		got := lookupCaption(captions, "s999", "p1")
		if got != "" {
			t.Errorf("expected empty, got '%s'", got)
		}
	})

	t.Run("photo not found", func(t *testing.T) {
		got := lookupCaption(captions, "s1", "p999")
		if got != "" {
			t.Errorf("expected empty, got '%s'", got)
		}
	})

	t.Run("nil map", func(t *testing.T) {
		got := lookupCaption(nil, "s1", "p1")
		if got != "" {
			t.Errorf("expected empty, got '%s'", got)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		got := lookupCaption(captionMap{}, "s1", "p1")
		if got != "" {
			t.Errorf("expected empty, got '%s'", got)
		}
	})
}

// --- getPageSlot ---

func TestGetPageSlot(t *testing.T) {
	page := database.BookPage{
		Slots: []database.PageSlot{
			{SlotIndex: 0, PhotoUID: "p0"},
			{SlotIndex: 1, PhotoUID: "p1"},
			{SlotIndex: 3, PhotoUID: "p3"},
		},
	}

	t.Run("found at index 0", func(t *testing.T) {
		got := getPageSlot(page, 0)
		if got.PhotoUID != "p0" {
			t.Errorf("expected 'p0', got '%s'", got.PhotoUID)
		}
	})

	t.Run("found at index 1", func(t *testing.T) {
		got := getPageSlot(page, 1)
		if got.PhotoUID != "p1" {
			t.Errorf("expected 'p1', got '%s'", got.PhotoUID)
		}
	})

	t.Run("found at index 3 (non-sequential)", func(t *testing.T) {
		got := getPageSlot(page, 3)
		if got.PhotoUID != "p3" {
			t.Errorf("expected 'p3', got '%s'", got.PhotoUID)
		}
	})

	t.Run("not found returns empty", func(t *testing.T) {
		got := getPageSlot(page, 99)
		if got.PhotoUID != "" {
			t.Errorf("expected empty, got '%s'", got.PhotoUID)
		}
		if got.SlotIndex != 99 {
			t.Errorf("expected slot index 99, got %d", got.SlotIndex)
		}
	})

	t.Run("empty slots", func(t *testing.T) {
		empty := database.BookPage{}
		got := getPageSlot(empty, 0)
		if got.PhotoUID != "" {
			t.Errorf("expected empty, got '%s'", got.PhotoUID)
		}
	})
}

// --- buildPhotoSlotNew ---

func TestBuildPhotoSlotNew(t *testing.T) {
	cfg := DefaultLayoutConfig()
	slot := SlotRect{X: 0, Y: 0, W: 130.5, H: 84.0}

	t.Run("landscape-in-landscape slot", func(t *testing.T) {
		img := photoImage{path: "/tmp/test.jpg", width: 3840, height: 2160}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, cfg.ArchivalInsetMM, 0.5, 0.5, 1.0)
		if !ts.HasPhoto {
			t.Error("expected HasPhoto=true")
		}
		// imgAspect = 3840/2160 = 1.778, slotAspect = 130.5/84 = 1.554
		// imgAspect > slotAspect → sizeDim="height"
		if ts.SizeDim != "height" {
			t.Errorf("expected height-binding for landscape-in-landscape, got %s", ts.SizeDim)
		}
		if ts.EffectiveDPI <= 0 {
			t.Errorf("expected positive DPI, got %.1f", ts.EffectiveDPI)
		}
		if ts.FilePath != "/tmp/test.jpg" {
			t.Errorf("expected /tmp/test.jpg, got '%s'", ts.FilePath)
		}
	})

	t.Run("portrait-in-portrait slot", func(t *testing.T) {
		portraitSlot := SlotRect{X: 0, Y: 0, W: 85.0, H: 172.0}
		img := photoImage{path: "/tmp/p.jpg", width: 2160, height: 3840}
		ts := buildPhotoSlotNew(portraitSlot, img, 20.0, 196.0, false, cfg.ArchivalInsetMM, 0.5, 0.5, 1.0)
		if !ts.HasPhoto {
			t.Error("expected HasPhoto=true")
		}
		if ts.SizeDim != "height" {
			t.Errorf("expected height-binding for portrait-in-portrait, got %s", ts.SizeDim)
		}
	})

	t.Run("cross-aspect: portrait image in landscape slot", func(t *testing.T) {
		img := photoImage{path: "/tmp/p.jpg", width: 2160, height: 3840}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, cfg.ArchivalInsetMM, 0.5, 0.5, 1.0)
		if !ts.HasPhoto {
			t.Error("expected HasPhoto=true")
		}
		// Portrait in landscape: image wider axis is height, slot is landscape
		// imgAspect (0.5625) < slotAspect (1.55) → sizeDim="width"
		if ts.SizeDim != "width" {
			t.Errorf("expected width-binding for portrait in landscape slot, got %s", ts.SizeDim)
		}
	})

	t.Run("archival inset", func(t *testing.T) {
		img := photoImage{path: "/tmp/a.jpg", width: 3840, height: 2160}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, true, 3.0, 0.5, 0.5, 1.0)
		if !ts.IsArchival {
			t.Error("expected IsArchival=true")
		}
		if math.Abs(ts.MatInsetMM-3.0) > eps {
			t.Errorf("expected inset 3.0, got %.2f", ts.MatInsetMM)
		}
		// Clip should be inset from border
		if math.Abs(ts.ClipW-(slot.W-6.0)) > eps {
			t.Errorf("expected clip width %.2f, got %.2f", slot.W-6.0, ts.ClipW)
		}
		if math.Abs(ts.ClipH-(slot.H-6.0)) > eps {
			t.Errorf("expected clip height %.2f, got %.2f", slot.H-6.0, ts.ClipH)
		}
		// Border should be the full slot
		if math.Abs(ts.BorderW-slot.W) > eps {
			t.Errorf("expected border width %.2f, got %.2f", slot.W, ts.BorderW)
		}
	})

	t.Run("crop offset top-left", func(t *testing.T) {
		img := photoImage{path: "/tmp/c.jpg", width: 3840, height: 2160}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.0, 0.0, 1.0)
		// cropX=0 → image shifted to left, cropY=0 → shifted to top
		// With 0.0 crop, the image start should be at clip boundary
		if ts.ImgX > ts.ClipX+eps {
			t.Errorf("expected ImgX <= ClipX for cropX=0, got ImgX=%.2f ClipX=%.2f", ts.ImgX, ts.ClipX)
		}
	})

	t.Run("crop offset bottom-right", func(t *testing.T) {
		img := photoImage{path: "/tmp/c.jpg", width: 3840, height: 2160}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 1.0, 1.0, 1.0)
		// cropX=1 → full overflow on left side
		// cropY=1 → full overflow on bottom side (TikZ Y inverted)
		if ts.HasPhoto != true {
			t.Error("expected HasPhoto=true")
		}
	})

	t.Run("zoom via cropScale", func(t *testing.T) {
		img := photoImage{path: "/tmp/z.jpg", width: 3840, height: 2160}
		tsNormal := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.5, 0.5, 1.0)
		tsZoomed := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.5, 0.5, 0.5)
		// Zoom in = smaller cropScale = larger sizeVal
		if tsZoomed.SizeVal <= tsNormal.SizeVal {
			t.Errorf("expected zoomed SizeVal > normal SizeVal, got %.2f <= %.2f", tsZoomed.SizeVal, tsNormal.SizeVal)
		}
	})

	t.Run("square image", func(t *testing.T) {
		img := photoImage{path: "/tmp/s.jpg", width: 2000, height: 2000}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.5, 0.5, 1.0)
		if !ts.HasPhoto {
			t.Error("expected HasPhoto=true")
		}
		// Square image in landscape slot: imgAspect(1.0) < slotAspect(1.55) → width binding
		if ts.SizeDim != "width" {
			t.Errorf("expected width-binding for square in landscape, got %s", ts.SizeDim)
		}
	})

	t.Run("low-res DPI", func(t *testing.T) {
		img := photoImage{path: "/tmp/lo.jpg", width: 400, height: 225}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.5, 0.5, 1.0)
		// 400px / (130.5mm / 25.4) = 400 / 5.138 ≈ 77.8 DPI
		if ts.EffectiveDPI >= lowResDPIThreshold {
			t.Errorf("expected low DPI (<%.0f), got %.1f", lowResDPIThreshold, ts.EffectiveDPI)
		}
	})
}

// --- buildTextSlotNew ---

func TestBuildTextSlotNew(t *testing.T) {
	t.Run("basic text", func(t *testing.T) {
		slot := SlotRect{X: 0, Y: 0, W: 130.5, H: 84.0}
		ps := database.PageSlot{TextContent: "Hello world"}
		ts := buildTextSlotNew(slot, ps, 20.0, 196.0)
		if !ts.HasText {
			t.Error("expected HasText=true")
		}
		if ts.TextContent != "Hello world" {
			t.Errorf("expected 'Hello world', got '%s'", ts.TextContent)
		}
		// ClipX = contentLeftX + slot.X = 20 + 0 = 20
		if math.Abs(ts.ClipX-20.0) > eps {
			t.Errorf("expected ClipX=20.0, got %.2f", ts.ClipX)
		}
		// ClipY = canvasTopY - slot.Y - slot.H = 196 - 0 - 84 = 112
		if math.Abs(ts.ClipY-112.0) > eps {
			t.Errorf("expected ClipY=112.0, got %.2f", ts.ClipY)
		}
	})

	t.Run("offset slot", func(t *testing.T) {
		slot := SlotRect{X: 50.0, Y: 10.0, W: 100.0, H: 80.0}
		ps := database.PageSlot{TextContent: "offset text"}
		ts := buildTextSlotNew(slot, ps, 20.0, 196.0)
		// ClipX = 20 + 50 = 70
		if math.Abs(ts.ClipX-70.0) > eps {
			t.Errorf("expected ClipX=70.0, got %.2f", ts.ClipX)
		}
		// ClipY = 196 - 10 - 80 = 106
		if math.Abs(ts.ClipY-106.0) > eps {
			t.Errorf("expected ClipY=106.0, got %.2f", ts.ClipY)
		}
	})

	t.Run("text type detection", func(t *testing.T) {
		tests := []struct {
			content  string
			expected string
		}{
			{"plain text", "T1"},
			{"- item 1\n- item 2", "T2"},
			{"> a quote", "T3"},
		}
		for _, tt := range tests {
			slot := SlotRect{X: 0, Y: 0, W: 100, H: 100}
			ps := database.PageSlot{TextContent: tt.content}
			ts := buildTextSlotNew(slot, ps, 20.0, 196.0)
			if ts.TextType != tt.expected {
				t.Errorf("content %q: expected type %s, got %s", tt.content, tt.expected, ts.TextType)
			}
		}
	})
}

// --- addDPIWarnings ---

func TestAddDPIWarnings(t *testing.T) {
	t.Run("no low-res photos", func(t *testing.T) {
		report := &ExportReport{
			Pages: []ReportPage{
				{PageNumber: 1, Photos: []ReportPhoto{
					{PhotoUID: "p1", EffectiveDPI: 300, LowRes: false},
				}},
			},
		}
		addDPIWarnings(report)
		if len(report.Warnings) != 0 {
			t.Errorf("expected 0 warnings, got %d", len(report.Warnings))
		}
	})

	t.Run("one low-res photo", func(t *testing.T) {
		report := &ExportReport{
			Pages: []ReportPage{
				{PageNumber: 1, Photos: []ReportPhoto{
					{PhotoUID: "p1", EffectiveDPI: 150, LowRes: true, SlotIndex: 0},
				}},
			},
		}
		addDPIWarnings(report)
		if len(report.Warnings) != 1 {
			t.Fatalf("expected 1 warning, got %d", len(report.Warnings))
		}
	})

	t.Run("mixed DPI", func(t *testing.T) {
		report := &ExportReport{
			Pages: []ReportPage{
				{PageNumber: 1, Photos: []ReportPhoto{
					{PhotoUID: "p1", EffectiveDPI: 300, LowRes: false},
					{PhotoUID: "p2", EffectiveDPI: 100, LowRes: true, SlotIndex: 1},
				}},
				{PageNumber: 2, Photos: []ReportPhoto{
					{PhotoUID: "p3", EffectiveDPI: 50, LowRes: true, SlotIndex: 0},
				}},
			},
		}
		addDPIWarnings(report)
		if len(report.Warnings) != 2 {
			t.Errorf("expected 2 warnings, got %d", len(report.Warnings))
		}
	})

	t.Run("zero DPI not warned", func(t *testing.T) {
		report := &ExportReport{
			Pages: []ReportPage{
				{PageNumber: 1, Photos: []ReportPhoto{
					{PhotoUID: "p1", EffectiveDPI: 0, LowRes: false},
				}},
			},
		}
		addDPIWarnings(report)
		if len(report.Warnings) != 0 {
			t.Errorf("expected 0 warnings for zero DPI, got %d", len(report.Warnings))
		}
	})

	t.Run("empty report", func(t *testing.T) {
		report := &ExportReport{}
		addDPIWarnings(report)
		if len(report.Warnings) != 0 {
			t.Errorf("expected 0 warnings, got %d", len(report.Warnings))
		}
	})
}

// --- buildTemplateData ---

func TestBuildTemplateData(t *testing.T) {
	t.Run("single section single page", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "Section 1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen",
					Slots: []database.PageSlot{{SlotIndex: 0, PhotoUID: "photo1"}}},
			}},
		}
		photos := map[string]photoImage{
			"photo1": {path: "/tmp/photo1.jpg", width: 3840, height: 2160},
		}
		data, report := buildTemplateData(groups, photos, nil, DefaultLayoutConfig(), "")

		if len(data.Sections) != 1 {
			t.Fatalf("expected 1 section, got %d", len(data.Sections))
		}
		if len(data.Sections[0].Pages) != 1 {
			t.Fatalf("expected 1 page, got %d", len(data.Sections[0].Pages))
		}
		if report.PhotoCount != 1 {
			t.Errorf("expected photo count 1, got %d", report.PhotoCount)
		}
		if report.PageCount != 1 {
			t.Errorf("expected page count 1, got %d", report.PageCount)
		}
	})

	t.Run("title page added when bookTitle set", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen"},
			}},
		}
		data, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), "My Book")

		if data.BookTitle != "My Book" {
			t.Errorf("expected book title 'My Book', got '%s'", data.BookTitle)
		}
		// Title page is page 1, content is page 2
		if report.PageCount != 2 {
			t.Errorf("expected 2 pages (title + content), got %d", report.PageCount)
		}
		if len(report.Pages) < 1 || report.Pages[0].Format != "title" {
			t.Error("expected first report page to be title format")
		}
		if !report.Pages[0].IsDivider {
			t.Error("expected title page to be divider")
		}
	})

	t.Run("no title page without bookTitle", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen"},
			}},
		}
		_, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), "")
		if report.PageCount != 1 {
			t.Errorf("expected 1 page (no title), got %d", report.PageCount)
		}
	})

	t.Run("multi-section", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "A", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen"},
			}},
			{sectionID: "s2", title: "B", pages: []database.BookPage{
				{ID: "p2", SectionID: "s2", Format: "1_fullscreen"},
			}},
		}
		data, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), "")

		if len(data.Sections) != 2 {
			t.Fatalf("expected 2 sections, got %d", len(data.Sections))
		}
		if data.Sections[0].Title != "A" || data.Sections[1].Title != "B" {
			t.Errorf("unexpected titles: %q, %q", data.Sections[0].Title, data.Sections[1].Title)
		}
		if report.PageCount != 2 {
			t.Errorf("expected 2 pages, got %d", report.PageCount)
		}
	})

	t.Run("page numbering and recto-verso", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen"},
				{ID: "p2", SectionID: "s1", Format: "1_fullscreen"},
				{ID: "p3", SectionID: "s1", Format: "1_fullscreen"},
			}},
		}
		data, _ := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), "")

		pages := data.Sections[0].Pages
		if pages[0].PageNumber != 1 || pages[1].PageNumber != 2 || pages[2].PageNumber != 3 {
			t.Errorf("unexpected page numbers: %d, %d, %d", pages[0].PageNumber, pages[1].PageNumber, pages[2].PageNumber)
		}
		// Odd=recto, even=verso
		if !pages[0].IsRecto {
			t.Error("page 1 should be recto")
		}
		if pages[1].IsRecto {
			t.Error("page 2 should be verso")
		}
		if !pages[2].IsRecto {
			t.Error("page 3 should be recto")
		}
	})

	t.Run("missing photo produces empty slot", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen",
					Slots: []database.PageSlot{{SlotIndex: 0, PhotoUID: "missing"}}},
			}},
		}
		data, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), "")
		slot := data.Sections[0].Pages[0].Slots[0]
		if slot.HasPhoto {
			t.Error("expected HasPhoto=false for missing photo")
		}
		if report.PhotoCount != 0 {
			t.Errorf("expected 0 photo count, got %d", report.PhotoCount)
		}
	})

	t.Run("section heading on first page", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "My Section", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen"},
				{ID: "p2", SectionID: "s1", Format: "1_fullscreen"},
			}},
		}
		data, _ := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), "")
		pages := data.Sections[0].Pages
		if !pages[0].HasSectionTitle {
			t.Error("first page should have section title")
		}
		if pages[0].SectionTitle != "My Section" {
			t.Errorf("expected section title 'My Section', got '%s'", pages[0].SectionTitle)
		}
		if pages[1].HasSectionTitle {
			t.Error("second page should NOT have section title")
		}
	})

	t.Run("photo count dedup", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "2_portrait",
					Slots: []database.PageSlot{
						{SlotIndex: 0, PhotoUID: "same"},
						{SlotIndex: 1, PhotoUID: "same"},
					}},
			}},
		}
		photos := map[string]photoImage{
			"same": {path: "/tmp/same.jpg", width: 2000, height: 3000},
		}
		_, report := buildTemplateData(groups, photos, nil, DefaultLayoutConfig(), "")
		if report.PhotoCount != 1 {
			t.Errorf("expected 1 unique photo, got %d", report.PhotoCount)
		}
	})
}

// --- latexEscapeRaw ---

func TestLatexEscapeRaw(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"backslash", `\`, `\textbackslash{}`},
		{"left brace", `{`, `\{`},
		{"right brace", `}`, `\}`},
		{"percent", `%`, `\%`},
		{"ampersand", `&`, `\&`},
		{"hash", `#`, `\#`},
		{"dollar", `$`, `\$`},
		{"underscore", `_`, `\_`},
		{"caret", `^`, `\textasciicircum{}`},
		{"tilde", `~`, `\textasciitilde{}`},
		{"empty string", "", ""},
		{"mixed", `Hello & "world" 100%`, `Hello \& "world" 100\%`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latexEscapeRaw(tt.input)
			if got != tt.expected {
				t.Errorf("latexEscapeRaw(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- czechTypography ---

func TestCzechTypography(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"preposition v", "v lese", "v~lese"},
		{"preposition k", "k domu", "k~domu"},
		{"preposition s", "s kamaradem", "s~kamaradem"},
		{"preposition z", "z mesta", "z~mesta"},
		{"preposition u", "u babicky", "u~babicky"},
		{"preposition o", "o zivote", "o~zivote"},
		{"preposition i", "i kdyz", "i~kdyz"},
		{"preposition a", "a proto", "a~proto"},
		{"uppercase", "V lese", "V~lese"},
		{"multi-letter words not matched", "ve meste", "ve meste"},
		{"start of string", "v lese", "v~lese"},
		{"mid-sentence", "pes a kocka", "pes a~kocka"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := czechTypography(tt.input)
			if got != tt.expected {
				t.Errorf("czechTypography(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- latexEscape ---

func TestLatexEscape(t *testing.T) {
	t.Run("combined escaping and typography", func(t *testing.T) {
		input := "100% v lese & 50$"
		got := latexEscape(input)
		// First escapes special chars, then applies typography
		expected := `100\% v~lese \& 50\$`
		if got != expected {
			t.Errorf("latexEscape(%q) = %q, want %q", input, got, expected)
		}
	})

	t.Run("no special chars", func(t *testing.T) {
		input := "plain text"
		got := latexEscape(input)
		if got != input {
			t.Errorf("latexEscape(%q) = %q, want %q", input, got, input)
		}
	})
}

// --- computeZones ---

func TestComputeZones(t *testing.T) {
	cfg := DefaultLayoutConfig()
	pb := &pageBuilder{config: cfg}

	t.Run("recto margins", func(t *testing.T) {
		contentLeftX, contentRightX, _, _, _, _, _, _, _ := pb.computeZones(true)
		// Recto: inside margin on left (20mm)
		if math.Abs(contentLeftX-cfg.InsideMarginMM) > eps {
			t.Errorf("recto contentLeftX: expected %.2f, got %.2f", cfg.InsideMarginMM, contentLeftX)
		}
		if math.Abs(contentRightX-(cfg.InsideMarginMM+cfg.ContentWidth())) > eps {
			t.Errorf("recto contentRightX: expected %.2f, got %.2f", cfg.InsideMarginMM+cfg.ContentWidth(), contentRightX)
		}
	})

	t.Run("verso margins", func(t *testing.T) {
		contentLeftX, contentRightX, _, _, _, _, _, _, _ := pb.computeZones(false)
		// Verso: outside margin on left (12mm)
		if math.Abs(contentLeftX-cfg.OutsideMarginMM) > eps {
			t.Errorf("verso contentLeftX: expected %.2f, got %.2f", cfg.OutsideMarginMM, contentLeftX)
		}
		if math.Abs(contentRightX-(cfg.OutsideMarginMM+cfg.ContentWidth())) > eps {
			t.Errorf("verso contentRightX: expected %.2f, got %.2f", cfg.OutsideMarginMM+cfg.ContentWidth(), contentRightX)
		}
	})

	t.Run("zone Y coordinates", func(t *testing.T) {
		_, _, headerY, canvasTopY, canvasBottomY, footerRuleY, _, _, _ := pb.computeZones(true)
		// topEdge = 210 - 10 = 200
		// headerY = 200 - 2 = 198
		if math.Abs(headerY-198.0) > eps {
			t.Errorf("headerY: expected 198.0, got %.2f", headerY)
		}
		// canvasTopY = 200 - 4 = 196
		if math.Abs(canvasTopY-196.0) > eps {
			t.Errorf("canvasTopY: expected 196.0, got %.2f", canvasTopY)
		}
		// canvasBottomY = 196 - 172 = 24
		if math.Abs(canvasBottomY-24.0) > eps {
			t.Errorf("canvasBottomY: expected 24.0, got %.2f", canvasBottomY)
		}
		// footerRuleY = canvasBottomY = 24
		if math.Abs(footerRuleY-24.0) > eps {
			t.Errorf("footerRuleY: expected 24.0, got %.2f", footerRuleY)
		}
	})

	t.Run("folio positioning recto", func(t *testing.T) {
		_, contentRightX, _, _, _, _, folioX, folioY, folioAnchor := pb.computeZones(true)
		// Recto: folio at bottom-right
		if math.Abs(folioX-contentRightX) > eps {
			t.Errorf("recto folioX: expected %.2f, got %.2f", contentRightX, folioX)
		}
		if math.Abs(folioY-cfg.BottomMarginMM/2.0) > eps {
			t.Errorf("recto folioY: expected %.2f, got %.2f", cfg.BottomMarginMM/2.0, folioY)
		}
		if folioAnchor != "south east" {
			t.Errorf("recto folioAnchor: expected 'south east', got '%s'", folioAnchor)
		}
	})

	t.Run("folio positioning verso", func(t *testing.T) {
		contentLeftX, _, _, _, _, _, folioX, _, folioAnchor := pb.computeZones(false)
		// Verso: folio at bottom-left
		if math.Abs(folioX-contentLeftX) > eps {
			t.Errorf("verso folioX: expected %.2f, got %.2f", contentLeftX, folioX)
		}
		if folioAnchor != "south west" {
			t.Errorf("verso folioAnchor: expected 'south west', got '%s'", folioAnchor)
		}
	})
}
