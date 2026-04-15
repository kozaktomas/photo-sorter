package latex

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"text/template"

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

// --- SortPagesBySectionOrder ---

func TestSortPagesBySectionOrder(t *testing.T) {
	t.Run("single section", func(t *testing.T) {
		pages := []database.BookPage{
			{ID: "p2", SectionID: "s1", SortOrder: 2},
			{ID: "p1", SectionID: "s1", SortOrder: 1},
		}
		sections := []database.BookSection{{ID: "s1"}}
		SortPagesBySectionOrder(pages, sections)
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
		SortPagesBySectionOrder(pages, sections)
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
		SortPagesBySectionOrder(pages, sections)
		if pages[0].ID != "p1" || pages[1].ID != "p2" || pages[2].ID != "p3" {
			t.Errorf("expected [p1, p2, p3], got [%s, %s, %s]", pages[0].ID, pages[1].ID, pages[2].ID)
		}
	})

	t.Run("empty pages", func(t *testing.T) {
		var pages []database.BookPage
		sections := []database.BookSection{{ID: "s1"}}
		SortPagesBySectionOrder(pages, sections) // should not panic
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
		groups := groupPagesBySection(pages, sections, nil)
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
		groups := groupPagesBySection(pages, sections, nil)
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
		groups := groupPagesBySection(pages, sections, nil)
		if len(groups) != 3 {
			t.Fatalf("expected 3 groups (alternating), got %d", len(groups))
		}
	})

	t.Run("empty", func(t *testing.T) {
		groups := groupPagesBySection(nil, nil, nil)
		if len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(groups))
		}
	})

	t.Run("missing title", func(t *testing.T) {
		pages := []database.BookPage{{SectionID: "s1"}}
		groups := groupPagesBySection(pages, nil, nil) // no sections with titles
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
	captions := CaptionMap{
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
		got := lookupCaption(CaptionMap{}, "s1", "p1")
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
		// imgAspect = 3840/2160 = 1.778, slotAspect = 130.5/84 = 1.554.
		// imgAspect > slotAspect → sizeDim="height".
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
		// Portrait in landscape: image wider axis is height, slot is landscape.
		// imgAspect (0.5625) < slotAspect (1.55) → sizeDim="width".
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
		// Clip should be inset from border.
		if math.Abs(ts.ClipW-(slot.W-6.0)) > eps {
			t.Errorf("expected clip width %.2f, got %.2f", slot.W-6.0, ts.ClipW)
		}
		if math.Abs(ts.ClipH-(slot.H-6.0)) > eps {
			t.Errorf("expected clip height %.2f, got %.2f", slot.H-6.0, ts.ClipH)
		}
		// Border should be the full slot.
		if math.Abs(ts.BorderW-slot.W) > eps {
			t.Errorf("expected border width %.2f, got %.2f", slot.W, ts.BorderW)
		}
	})

	t.Run("crop offset top-left", func(t *testing.T) {
		img := photoImage{path: "/tmp/c.jpg", width: 3840, height: 2160}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.0, 0.0, 1.0)
		// cropX=0 → image shifted to left, cropY=0 → shifted to top.
		// With 0.0 crop, the image start should be at clip boundary.
		if ts.ImgX > ts.ClipX+eps {
			t.Errorf("expected ImgX <= ClipX for cropX=0, got ImgX=%.2f ClipX=%.2f", ts.ImgX, ts.ClipX)
		}
	})

	t.Run("crop offset bottom-right", func(t *testing.T) {
		img := photoImage{path: "/tmp/c.jpg", width: 3840, height: 2160}
		ts := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 1.0, 1.0, 1.0)
		// cropX=1 → full overflow on left side.
		// cropY=1 → full overflow on bottom side (TikZ Y inverted).
		if ts.HasPhoto != true {
			t.Error("expected HasPhoto=true")
		}
	})

	t.Run("zoom via cropScale", func(t *testing.T) {
		img := photoImage{path: "/tmp/z.jpg", width: 3840, height: 2160}
		tsNormal := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.5, 0.5, 1.0)
		tsZoomed := buildPhotoSlotNew(slot, img, 20.0, 196.0, false, 0, 0.5, 0.5, 0.5)
		// Zoom in = smaller cropScale = larger sizeVal.
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
		// Square image in landscape slot: imgAspect(1.0) < slotAspect(1.55) → width binding.
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
		// ClipX = contentLeftX + slot.X = 20 + 0 = 20.
		if math.Abs(ts.ClipX-20.0) > eps {
			t.Errorf("expected ClipX=20.0, got %.2f", ts.ClipX)
		}
		// ClipY = canvasTopY - slot.Y - slot.H = 196 - 0 - 84 = 112.
		if math.Abs(ts.ClipY-112.0) > eps {
			t.Errorf("expected ClipY=112.0, got %.2f", ts.ClipY)
		}
	})

	t.Run("offset slot", func(t *testing.T) {
		slot := SlotRect{X: 50.0, Y: 10.0, W: 100.0, H: 80.0}
		ps := database.PageSlot{TextContent: "offset text"}
		ts := buildTextSlotNew(slot, ps, 20.0, 196.0)
		// ClipX = 20 + 50 = 70.
		if math.Abs(ts.ClipX-70.0) > eps {
			t.Errorf("expected ClipX=70.0, got %.2f", ts.ClipX)
		}
		// ClipY = 196 - 10 - 80 = 106.
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

// dummySlot returns a minimal non-empty slot so the page is not skipped as empty.
func dummySlot() []database.PageSlot {
	return []database.PageSlot{{SlotIndex: 0, TextContent: "x"}}
}

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
		data, report := buildTemplateData(groups, photos, nil, DefaultLayoutConfig(), nil)

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

	t.Run("no title page even when bookTitle set", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
			}},
		}
		_, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), &database.PhotoBook{Title: "My Book"})

		// No title page — content starts at page 1.
		if report.PageCount != 1 {
			t.Errorf("expected 1 page (content only), got %d", report.PageCount)
		}
		if len(report.Pages) > 0 && report.Pages[0].Format == "title" {
			t.Error("expected no title page")
		}
	})

	t.Run("no title page without bookTitle", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
			}},
		}
		_, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), nil)
		if report.PageCount != 1 {
			t.Errorf("expected 1 page (no title), got %d", report.PageCount)
		}
	})

	t.Run("multi-section", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "A", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
			}},
			{sectionID: "s2", title: "B", pages: []database.BookPage{
				{ID: "p2", SectionID: "s2", Format: "1_fullscreen", Slots: dummySlot()},
			}},
		}
		data, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), nil)

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
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
				{ID: "p2", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
				{ID: "p3", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
			}},
		}
		data, _ := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), nil)

		pages := data.Sections[0].Pages
		if pages[0].PageNumber != 1 || pages[1].PageNumber != 2 || pages[2].PageNumber != 3 {
			t.Errorf("unexpected page numbers: %d, %d, %d", pages[0].PageNumber, pages[1].PageNumber, pages[2].PageNumber)
		}
		// Odd=recto, even=verso.
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
		data, report := buildTemplateData(groups, nil, nil, DefaultLayoutConfig(), nil)
		slot := data.Sections[0].Pages[0].Slots[0]
		if slot.HasPhoto {
			t.Error("expected HasPhoto=false for missing photo")
		}
		if report.PhotoCount != 0 {
			t.Errorf("expected 0 photo count, got %d", report.PhotoCount)
		}
	})

	t.Run("titled section uses full canvas height", func(t *testing.T) {
		groups := []sectionGroup{
			{sectionID: "s1", title: "My Section", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
				{ID: "p2", SectionID: "s1", Format: "1_fullscreen", Slots: dummySlot()},
			}},
		}
		cfg := DefaultLayoutConfig()
		data, _ := buildTemplateData(groups, nil, nil, cfg, nil)
		pages := data.Sections[0].Pages
		// Both pages should use the full canvas top Y (no section heading reduction).
		expectedCanvasTopY := PageH - cfg.TopMarginMM - cfg.HeaderHeightMM
		if pages[0].CanvasTopY != expectedCanvasTopY {
			t.Errorf("first page CanvasTopY = %.2f, want %.2f", pages[0].CanvasTopY, expectedCanvasTopY)
		}
		if pages[1].CanvasTopY != expectedCanvasTopY {
			t.Errorf("second page CanvasTopY = %.2f, want %.2f", pages[1].CanvasTopY, expectedCanvasTopY)
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
		_, report := buildTemplateData(groups, photos, nil, DefaultLayoutConfig(), nil)
		if report.PhotoCount != 1 {
			t.Errorf("expected 1 unique photo, got %d", report.PhotoCount)
		}
	})

	t.Run("empty pages are preserved in pagination", func(t *testing.T) {
		// Middle page has zero slots — historically this was dropped from
		// the export and the third page got folio 2. It must now survive
		// as a blank page with folio 2, and the third page gets folio 3.
		groups := []sectionGroup{
			{sectionID: "s1", title: "S1", pages: []database.BookPage{
				{ID: "p1", SectionID: "s1", Format: "1_fullscreen",
					Slots: []database.PageSlot{{SlotIndex: 0, PhotoUID: "photo1"}}},
				{ID: "p2", SectionID: "s1", Format: "1_fullscreen"}, // no slots
				{ID: "p3", SectionID: "s1", Format: "1_fullscreen",
					Slots: []database.PageSlot{{SlotIndex: 0, PhotoUID: "photo3"}}},
			}},
		}
		photos := map[string]photoImage{
			"photo1": {path: "/tmp/photo1.jpg", width: 3840, height: 2160},
			"photo3": {path: "/tmp/photo3.jpg", width: 3840, height: 2160},
		}
		data, report := buildTemplateData(groups, photos, nil, DefaultLayoutConfig(), nil)

		pages := data.Sections[0].Pages
		if len(pages) != 3 {
			t.Fatalf("expected 3 pages, got %d", len(pages))
		}
		if pages[0].PageNumber != 1 || pages[1].PageNumber != 2 || pages[2].PageNumber != 3 {
			t.Errorf("unexpected page numbers: %d, %d, %d",
				pages[0].PageNumber, pages[1].PageNumber, pages[2].PageNumber)
		}
		if !pages[2].IsLast {
			t.Error("last page should be marked IsLast")
		}
		if pages[0].IsLast || pages[1].IsLast {
			t.Error("only the final page should be IsLast")
		}
		if report.PageCount != 3 {
			t.Errorf("expected PageCount=3, got %d", report.PageCount)
		}
		// The blank middle page has a valid tikz frame with no photo/text
		// slots — all slot entries must have HasPhoto=false and HasText=false.
		for i, s := range pages[1].Slots {
			if s.HasPhoto || s.HasText {
				t.Errorf("middle empty page slot[%d] unexpectedly has content", i)
			}
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
		// First escapes special chars, then applies typography.
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

// --- latexEscapeCaptionRaw ---

func TestLatexEscapeCaptionRaw(t *testing.T) {
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
		{"tilde preserved", `~`, `~`},
		{"empty string", "", ""},
		{"tilde between words", `100~ml`, `100~ml`},
		{"mixed", `Hello & 100% ~world`, `Hello \& 100\% ~world`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latexEscapeCaptionRaw(tt.input)
			if got != tt.expected {
				t.Errorf("latexEscapeCaptionRaw(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- latexEscapeCaption ---

func TestLatexEscapeCaption(t *testing.T) {
	t.Run("bold", func(t *testing.T) {
		got := latexEscapeCaption(`**bold**`)
		want := `\textbf{bold}`
		if got != want {
			t.Errorf("latexEscapeCaption(%q) = %q, want %q", `**bold**`, got, want)
		}
	})

	t.Run("italic", func(t *testing.T) {
		got := latexEscapeCaption(`*italic*`)
		want := `\textit{italic}`
		if got != want {
			t.Errorf("latexEscapeCaption(%q) = %q, want %q", `*italic*`, got, want)
		}
	})

	t.Run("bold then italic on separate words", func(t *testing.T) {
		got := latexEscapeCaption(`**one** two *three*`)
		want := `\textbf{one} two \textit{three}`
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("tilde preserved as non-breaking space", func(t *testing.T) {
		got := latexEscapeCaption(`100~ml`)
		want := `100~ml`
		if got != want {
			t.Errorf("latexEscapeCaption(%q) = %q, want %q", `100~ml`, got, want)
		}
	})

	t.Run("bare tilde preserved", func(t *testing.T) {
		got := latexEscapeCaption(`~`)
		want := `~`
		if got != want {
			t.Errorf("latexEscapeCaption(%q) = %q, want %q", `~`, got, want)
		}
	})

	t.Run("other LaTeX specials still escaped", func(t *testing.T) {
		got := latexEscapeCaption(`Bill & Ted, 50% #tag $price`)
		want := `Bill \& Ted, 50\% \#tag \$price`
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("caret still escaped", func(t *testing.T) {
		got := latexEscapeCaption(`a^b`)
		want := `a\textasciicircum{}b`
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("backslash still escaped", func(t *testing.T) {
		got := latexEscapeCaption(`a\b`)
		want := `a\textbackslash{}b`
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("czech typography still applied", func(t *testing.T) {
		got := latexEscapeCaption("v lese")
		want := "v~lese"
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("czech typography applies outside bold", func(t *testing.T) {
		// Inside \textbf{...} the `v` is preceded by `{`, which is neither
		// start-of-string nor whitespace, so czechTypographyRe does not match.
		// Outside bold, `a` is preceded by a space and gets the NBSP.
		got := latexEscapeCaption("**v lese** a doma")
		want := `\textbf{v lese} a~doma`
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("bold combined with tilde and ampersand", func(t *testing.T) {
		got := latexEscapeCaption(`**Pepa & Jana** 100~ml`)
		want := `\textbf{Pepa \& Jana} 100~ml`
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("no formatting plain text", func(t *testing.T) {
		got := latexEscapeCaption("Just text")
		want := "Just text"
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		got := latexEscapeCaption("")
		want := ""
		if got != want {
			t.Errorf("latexEscapeCaption = %q, want %q", got, want)
		}
	})
}

// --- computeZones ---

func TestComputeZones(t *testing.T) {
	cfg := DefaultLayoutConfig()
	pb := &pageBuilder{config: cfg}

	t.Run("recto margins", func(t *testing.T) {
		contentLeftX, contentRightX, _, _, _, _, _, _, _ := pb.computeZones(true)
		// Recto: inside margin on left (20mm).
		if math.Abs(contentLeftX-cfg.InsideMarginMM) > eps {
			t.Errorf("recto contentLeftX: expected %.2f, got %.2f", cfg.InsideMarginMM, contentLeftX)
		}
		if math.Abs(contentRightX-(cfg.InsideMarginMM+cfg.ContentWidth())) > eps {
			t.Errorf("recto contentRightX: expected %.2f, got %.2f", cfg.InsideMarginMM+cfg.ContentWidth(), contentRightX)
		}
	})

	t.Run("verso margins", func(t *testing.T) {
		contentLeftX, contentRightX, _, _, _, _, _, _, _ := pb.computeZones(false)
		// Verso: outside margin on left (12mm).
		if math.Abs(contentLeftX-cfg.OutsideMarginMM) > eps {
			t.Errorf("verso contentLeftX: expected %.2f, got %.2f", cfg.OutsideMarginMM, contentLeftX)
		}
		if math.Abs(contentRightX-(cfg.OutsideMarginMM+cfg.ContentWidth())) > eps {
			t.Errorf("verso contentRightX: expected %.2f, got %.2f", cfg.OutsideMarginMM+cfg.ContentWidth(), contentRightX)
		}
	})

	t.Run("zone Y coordinates", func(t *testing.T) {
		_, _, headerY, canvasTopY, canvasBottomY, footerRuleY, _, _, _ := pb.computeZones(true)
		// topEdge = 210 - 10 = 200.
		// headerY = 200 - 2 = 198.
		if math.Abs(headerY-198.0) > eps {
			t.Errorf("headerY: expected 198.0, got %.2f", headerY)
		}
		// canvasTopY = 200 - 4 = 196.
		if math.Abs(canvasTopY-196.0) > eps {
			t.Errorf("canvasTopY: expected 196.0, got %.2f", canvasTopY)
		}
		// canvasBottomY = 196 - 172 = 24.
		if math.Abs(canvasBottomY-24.0) > eps {
			t.Errorf("canvasBottomY: expected 24.0, got %.2f", canvasBottomY)
		}
		// footerRuleY = canvasBottomY = 24.
		if math.Abs(footerRuleY-24.0) > eps {
			t.Errorf("footerRuleY: expected 24.0, got %.2f", footerRuleY)
		}
	})

	t.Run("folio positioning recto", func(t *testing.T) {
		_, contentRightX, _, _, _, _, folioX, folioY, folioAnchor := pb.computeZones(true)
		// Recto: folio at bottom-right.
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
		// Verso: folio at bottom-left.
		if math.Abs(folioX-contentLeftX) > eps {
			t.Errorf("verso folioX: expected %.2f, got %.2f", contentLeftX, folioX)
		}
		if folioAnchor != "south west" {
			t.Errorf("verso folioAnchor: expected 'south west', got '%s'", folioAnchor)
		}
	})
}

// --- mergeFooterCaptions ---

// equalInts is a small helper for slice comparison in tests.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMergeFooterCaptions(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := mergeFooterCaptions(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("single caption", func(t *testing.T) {
		caps := []FooterCaption{{Markers: []int{1}, Caption: "Photo A"}}
		got := mergeFooterCaptions(caps)
		if len(got) != 1 || !equalInts(got[0].Markers, []int{1}) || got[0].Caption != "Photo A" {
			t.Errorf("unexpected result: %v", got)
		}
	})

	t.Run("all same caption keeps individual markers", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: []int{1}, Caption: "Same"},
			{Markers: []int{2}, Caption: "Same"},
			{Markers: []int{3}, Caption: "Same"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 1 {
			t.Fatalf("expected 1 merged caption, got %d", len(got))
		}
		if !equalInts(got[0].Markers, []int{1, 2, 3}) {
			t.Errorf("markers = %v, want [1 2 3]", got[0].Markers)
		}
		if got[0].Caption != "Same" {
			t.Errorf("caption = %q, want %q", got[0].Caption, "Same")
		}
	})

	t.Run("all different captions", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: []int{1}, Caption: "A"},
			{Markers: []int{2}, Caption: "B"},
			{Markers: []int{3}, Caption: "C"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 3 {
			t.Fatalf("expected 3 captions, got %d", len(got))
		}
		for i, want := range [][]int{{1}, {2}, {3}} {
			if !equalInts(got[i].Markers, want) {
				t.Errorf("got[%d].Markers = %v, want %v", i, got[i].Markers, want)
			}
		}
	})

	t.Run("partial merge non-consecutive", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: []int{1}, Caption: "Same"},
			{Markers: []int{2}, Caption: "Different"},
			{Markers: []int{3}, Caption: "Same"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 2 {
			t.Fatalf("expected 2 captions, got %d", len(got))
		}
		if !equalInts(got[0].Markers, []int{1, 3}) || got[0].Caption != "Same" {
			t.Errorf("got[0] = {%v, %q}, want {[1 3], %q}", got[0].Markers, got[0].Caption, "Same")
		}
		if !equalInts(got[1].Markers, []int{2}) || got[1].Caption != "Different" {
			t.Errorf("got[1] = {%v, %q}, want {[2], %q}", got[1].Markers, got[1].Caption, "Different")
		}
	})

	t.Run("preserves first occurrence order", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: []int{1}, Caption: "B"},
			{Markers: []int{2}, Caption: "A"},
			{Markers: []int{3}, Caption: "B"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 2 {
			t.Fatalf("expected 2 captions, got %d", len(got))
		}
		// B appeared first, so it should come first
		if got[0].Caption != "B" {
			t.Errorf("expected first caption to be B, got %q", got[0].Caption)
		}
		if got[1].Caption != "A" {
			t.Errorf("expected second caption to be A, got %q", got[1].Caption)
		}
	})

	t.Run("merged markers are sorted", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: []int{3}, Caption: "Same"},
			{Markers: []int{1}, Caption: "Same"},
			{Markers: []int{2}, Caption: "Same"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 1 || !equalInts(got[0].Markers, []int{1, 2, 3}) {
			t.Errorf("expected sorted [1 2 3], got %v", got[0].Markers)
		}
	})

	t.Run("no markers", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: nil, Caption: "Solo"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 1 || len(got[0].Markers) != 0 {
			t.Errorf("unexpected result: %v", got)
		}
	})

	t.Run("preserves chapter color", func(t *testing.T) {
		caps := []FooterCaption{
			{Markers: []int{1}, Caption: "A", ChapterColor: "8B0000"},
			{Markers: []int{2}, Caption: "A", ChapterColor: "8B0000"},
			{Markers: []int{3}, Caption: "B", ChapterColor: "8B0000"},
		}
		got := mergeFooterCaptions(caps)
		if len(got) != 2 {
			t.Fatalf("expected 2 merged captions, got %d", len(got))
		}
		for i, c := range got {
			if c.ChapterColor != "8B0000" {
				t.Errorf("got[%d].ChapterColor = %q, want %q", i, c.ChapterColor, "8B0000")
			}
		}
	})
}

// --- book.tex caption badge rendering ---

// renderBookTemplate parses and executes the embedded book.tex template against
// the given TemplateData and returns the rendered LaTeX source. It uses the
// same funcMap as compileLatex so caption-badge branches are exercised
// end-to-end without invoking lualatex.
func renderBookTemplate(t *testing.T, data TemplateData) string {
	t.Helper()
	funcMap := bookTemplateFuncMap(TypographyConfig{
		H1Size: data.H1FontSize, H1Leading: data.H1Leading,
		H2Size: data.H2FontSize, H2Leading: data.H2Leading,
	})
	tmpl, err := template.New("book.tex").Funcs(funcMap).ParseFS(templateFS, "templates/book.tex")
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return buf.String()
}

func TestCaptionBadgeTemplate(t *testing.T) {
	makeData := func(captions []FooterCaption) TemplateData {
		return TemplateData{
			Sections: []TemplateSection{{
				Pages: []TemplatePage{{
					IsLast:      true,
					PageNumber:  1,
					Style:       "modern",
					HasCaptions: true,
					Captions:    captions,
					FolioAnchor: "south east",
				}},
			}},
			BodyFontDeclaration:    `\setmainfont{PT Serif}[` + "\n  Ligatures=TeX,\n]",
			HeadingFontDeclaration: `\setsansfont{Source Sans 3}[` + "\n  Ligatures=TeX,\n]",
			BodyFontSize:           11, BodyLineHeight: 15,
			H1FontSize: 18, H1Leading: 22,
			H2FontSize: 13, H2Leading: 16,
			CaptionOpacity: 50, CaptionFontSize: 9, CaptionLeading: 11,
			CaptionBadgeSize: 4.0,
		}
	}

	// captionLine extracts the caption tikz node line that holds the marker badges,
	// so assertions ignore the preamble \newcommand definitions.
	captionLine := func(out string) string {
		for line := range strings.SplitSeq(out, "\n") {
			if strings.Contains(line, `\captionbadge`) && strings.Contains(line, "Photo A") {
				return line
			}
			if strings.Contains(line, `\captionbadgefb`) && strings.Contains(line, "Photo A") {
				return line
			}
		}
		return ""
	}

	t.Run("badge with chapter color uses captionbadge", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Photo A", ChapterColor: "8B0000"},
		}))
		line := captionLine(out)
		// Args: size_mm, font_pt (= size×1.5), bg_hex, text_color, marker.
		if !strings.Contains(line, `\captionbadge{4.00}{6.00}{8B0000}{white}{1}`) {
			t.Errorf("expected \\captionbadge{4.00}{6.00}{8B0000}{white}{1} in caption line, got: %q", line)
		}
		if strings.Contains(line, `\captionbadgefb{`) {
			t.Errorf("did not expect fallback badge usage in caption line, got: %q", line)
		}
	})

	t.Run("badge without chapter color uses fallback", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Photo A"},
		}))
		line := captionLine(out)
		if !strings.Contains(line, `\captionbadgefb{4.00}{6.00}{1}`) {
			t.Errorf("expected \\captionbadgefb{4.00}{6.00}{1} in caption line, got: %q", line)
		}
	})

	t.Run("shared caption renders one badge per marker", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1, 2, 3}, Caption: "Same caption", ChapterColor: "FFFFFF"},
		}))
		// White background should pick black text via contrastTextColor.
		// Three photos sharing one caption produce three side-by-side badges
		// separated by \thinspace.
		want := `\captionbadge{4.00}{6.00}{FFFFFF}{black}{1}\thinspace \captionbadge{4.00}{6.00}{FFFFFF}{black}{2}\thinspace \captionbadge{4.00}{6.00}{FFFFFF}{black}{3}`
		if !strings.Contains(out, want) {
			t.Errorf("expected per-marker badges %q, got:\n%s", want, out)
		}
	})

	t.Run("custom badge size flows into badge command and scales font", func(t *testing.T) {
		data := makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Photo A", ChapterColor: "8B0000"},
		})
		data.CaptionBadgeSize = 6.0 // 6 mm → 9 pt font
		out := renderBookTemplate(t, data)
		if !strings.Contains(out, `\captionbadge{6.00}{9.00}{8B0000}{white}{1}`) {
			t.Errorf("expected custom size 6mm with 9pt font, got:\n%s", out)
		}
	})

	t.Run("preamble defines captionbadge command", func(t *testing.T) {
		out := renderBookTemplate(t, makeData(nil))
		if !strings.Contains(out, `\newcommand{\captionbadge}`) {
			t.Errorf("expected \\captionbadge definition in preamble")
		}
		if !strings.Contains(out, `\newcommand{\captionbadgefb}`) {
			t.Errorf("expected \\captionbadgefb definition in preamble")
		}
	})

	t.Run("each caption is wrapped in mbox so it stays on one line", func(t *testing.T) {
		// Two captions on the same page; LaTeX must only consider the
		// inter-caption \quad as a break point, never a space inside a
		// caption. Wrapping each caption (badges + text) in \mbox{...}
		// achieves that.
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Photo A", ChapterColor: "8B0000"},
			{Markers: []int{2}, Caption: "Photo B with longer text", ChapterColor: "8B0000"},
		}))
		wantA := `\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{1}\, Photo A}`
		wantB := `\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{2}\, Photo B with longer text}`
		if !strings.Contains(out, wantA) {
			t.Errorf("expected first caption wrapped as %q, got:\n%s", wantA, out)
		}
		if !strings.Contains(out, wantB) {
			t.Errorf("expected second caption wrapped as %q, got:\n%s", wantB, out)
		}
		// And the two captions are still separated by \quad.
		if !strings.Contains(out, wantA+`\quad `+wantB) {
			t.Errorf("expected captions joined by \\quad, got:\n%s", out)
		}
		// The caption block must disable microtype font expansion so TeX
		// cannot silently squeeze a too-long caption line by 1-2% to make
		// it "fit" within the target width.
		if !strings.Contains(out, `\microtypesetup{expansion=false}`) {
			t.Errorf("expected \\microtypesetup{expansion=false} in caption block, got:\n%s", out)
		}
	})

	t.Run("inline newline in caption text becomes hard break with stacked mboxes", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Line 1\nLine 2", ChapterColor: "8B0000"},
		}))
		want := `\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{1}\, Line 1}\\\mbox{Line 2}`
		if !strings.Contains(out, want) {
			t.Errorf("expected stacked mboxes %q, got:\n%s", want, out)
		}
	})

	t.Run("trailing newline forces a break and skips inter-caption quad", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Photo A\n", ChapterColor: "8B0000"},
			{Markers: []int{2}, Caption: "Photo B", ChapterColor: "8B0000"},
		}))
		// Trailing \n on cap1 produces a bare \\ (no empty mbox), and cap2
		// follows directly with no leading \quad — so the new line starts
		// at the left margin instead of being indented by 1em.
		want := `\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{1}\, Photo A}\\\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{2}\, Photo B}`
		if !strings.Contains(out, want) {
			t.Errorf("expected break with no leading quad %q, got:\n%s", want, out)
		}
		// Sanity: there must NOT be a \quad between the trailing-break caption
		// and the next one.
		bad := `\\\quad `
		if strings.Contains(out, bad) {
			t.Errorf("did not expect %q in output, got:\n%s", bad, out)
		}
	})

	t.Run("plain caption without newlines is unchanged", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "Just text", ChapterColor: "8B0000"},
		}))
		want := `\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{1}\, Just text}`
		if !strings.Contains(out, want) {
			t.Errorf("expected plain mbox %q, got:\n%s", want, out)
		}
	})

	t.Run("caption bold italic and tilde render through template", func(t *testing.T) {
		out := renderBookTemplate(t, makeData([]FooterCaption{
			{Markers: []int{1}, Caption: "**Bold** and *italic* with 100~ml", ChapterColor: "8B0000"},
		}))
		want := `\mbox{\captionbadge{4.00}{6.00}{8B0000}{white}{1}\, \textbf{Bold} and \textit{italic} with 100~ml}`
		if !strings.Contains(out, want) {
			t.Errorf("expected formatted caption %q in output:\n%s", want, out)
		}
	})
}

// --- Captions slot ---

// buildCaptionsSlotPageData assembles TemplateData with a 4_landscape page:
// slots 0 and 1 hold photos, slot 3 is the captions slot. Both photos have
// captions in the CaptionMap so the captions slot list contains 2 entries.
func buildCaptionsSlotPageData(t *testing.T) (TemplateData, string) {
	t.Helper()
	captions := CaptionMap{
		"s1": {
			"photoA": "Long caption that wouldn't fit in the bottom strip",
			"photoB": "Second caption",
		},
	}
	groups := []sectionGroup{
		{sectionID: "s1", title: "S1", chapterColor: "8B0000", pages: []database.BookPage{
			{
				ID:        "p1",
				SectionID: "s1",
				Format:    "4_landscape",
				Slots: []database.PageSlot{
					{SlotIndex: 0, PhotoUID: "photoA"},
					{SlotIndex: 1, PhotoUID: "photoB"},
					{SlotIndex: 3, IsCaptionsSlot: true},
				},
			},
		}},
	}
	photos := map[string]photoImage{
		"photoA": {path: "/tmp/photoA.jpg", width: 2000, height: 3000},
		"photoB": {path: "/tmp/photoB.jpg", width: 2000, height: 3000},
	}
	data, _ := buildTemplateData(groups, photos, captions, DefaultLayoutConfig(), nil)
	return data, "photoA"
}

func TestCaptionsSlot_SuppressesFooter(t *testing.T) {
	data, _ := buildCaptionsSlotPageData(t)

	page := data.Sections[0].Pages[0]

	// Footer caption block must be suppressed for this page.
	if page.HasCaptions {
		t.Error("expected HasCaptions=false when a captions slot is present")
	}
	if len(page.Captions) != 0 {
		t.Errorf("expected empty page.Captions, got %d", len(page.Captions))
	}

	// Slot 3 must be the captions slot with the caption list routed into it.
	if len(page.Slots) < 4 {
		t.Fatalf("expected at least 4 slots, got %d", len(page.Slots))
	}
	captionsSlot := page.Slots[3]
	if !captionsSlot.HasCaptionsList {
		t.Error("expected slot 3 to be a captions slot (HasCaptionsList=true)")
	}
	if captionsSlot.HasPhoto || captionsSlot.HasText {
		t.Error("captions slot must not also be flagged HasPhoto or HasText")
	}
	if len(captionsSlot.CaptionsList) != 2 {
		t.Errorf("expected 2 captions in CaptionsList, got %d", len(captionsSlot.CaptionsList))
	}
	// Slots 0 and 1 must still render their photos normally.
	if !page.Slots[0].HasPhoto {
		t.Error("expected slot 0 to still be a photo slot")
	}
	if !page.Slots[1].HasPhoto {
		t.Error("expected slot 1 to still be a photo slot")
	}
}

func TestCaptionsSlot_RendersThroughTemplate(t *testing.T) {
	data, _ := buildCaptionsSlotPageData(t)
	// Populate typography defaults so the template renders.
	data.BodyFontDeclaration = `\setmainfont{PT Serif}[Ligatures=TeX,]`
	data.HeadingFontDeclaration = `\setsansfont{Source Sans 3}[Ligatures=TeX,]`
	data.BodyFontSize = 11
	data.BodyLineHeight = 15
	data.H1FontSize = 18
	data.H1Leading = 22
	data.H2FontSize = 13
	data.H2Leading = 16
	data.CaptionOpacity = 50
	data.CaptionFontSize = 9
	data.CaptionLeading = 11
	data.CaptionBadgeSize = 4.0

	out := renderBookTemplate(t, data)

	// The bottom caption strip (renderFooterCaption) must NOT be emitted for
	// this page — the only way it would appear is via `{{- renderFooterCaption}}`
	// inside a minipage, so check that the footer-specific marker is absent.
	if strings.Contains(out, `renderFooterCaption`) {
		t.Error("template still references renderFooterCaption at runtime — should be consumed")
	}
	// The page's footer block is guarded by `{{- if $page.HasCaptions}}`,
	// which is false here, so the `\microtypesetup{expansion=false}` block
	// that wraps the footer caption node must NOT appear. (The in-slot
	// captions minipage is separately guarded by $slot.HasCaptionsList.)
	// The minipage itself is an inline marker we can look for:
	if !strings.Contains(out, `% Captions slot`) {
		t.Errorf("expected captions-slot LaTeX block in output, got:\n%s", out)
	}
	// The caption text must render inside the slot.
	if !strings.Contains(out, "Long caption that wouldn't fit in the bottom strip") {
		t.Error("expected caption text to be rendered inside the captions slot")
	}
	// Each caption must set hangindent so wrapped lines align with caption
	// text (not the badge). Default badge size is 4mm + 1.5mm gap = 5.5mm.
	if !strings.Contains(out, `\hangindent=5.50mm`) {
		t.Errorf("expected \\hangindent=5.50mm before each caption, got:\n%s", out)
	}
	// The badge gap must use a fixed-width \hspace so wrapped lines align
	// exactly with the start of the caption text on line 1.
	if !strings.Contains(out, `\hspace{1.50mm}`) {
		t.Errorf("expected \\hspace{1.50mm} as the badge-to-text gap, got:\n%s", out)
	}
	// Captions must be separated by `\par` so they actually stack vertically
	// rather than flowing as one paragraph. There are 2 captions in the
	// test fixture, so we expect at least one `\par` between them.
	if strings.Count(out, `\par `) < 1 {
		t.Errorf("expected `\\par ` between stacked captions, got:\n%s", out)
	}
	// Caption text must be justified inside the slot (no \raggedright). We
	// look for the captions slot block specifically and assert \raggedright
	// is absent after the parbox setup line.
	captionsSection := out
	if idx := strings.Index(out, "% Captions slot"); idx >= 0 {
		captionsSection = out[idx:]
		if end := strings.Index(captionsSection, "\\end{scope}"); end >= 0 {
			captionsSection = captionsSection[:end]
		}
	}
	if strings.Contains(captionsSection, `\raggedright`) {
		t.Errorf("captions slot must render justified text, found \\raggedright in:\n%s", captionsSection)
	}
	// Critical regression guard: there must NOT be a `%` immediately after
	// any caption text — that would comment out the next iteration's
	// `\par`/`\noindent` and make all captions render as a single paragraph.
	for _, fc := range []string{"Long caption that wouldn't fit in the bottom strip", "Second caption"} {
		if strings.Contains(out, fc+`%`) {
			t.Errorf("caption %q must not be followed by a `%%` comment marker", fc)
		}
	}
}

func TestCaptionsSlot_NoCaptionsStillBuilds(t *testing.T) {
	// Captions slot on a page with no section_photo descriptions — should
	// still render the slot (as an empty parbox) without panicking.
	groups := []sectionGroup{
		{sectionID: "s1", title: "S1", pages: []database.BookPage{
			{
				ID:        "p1",
				SectionID: "s1",
				Format:    "2_portrait",
				Slots: []database.PageSlot{
					{SlotIndex: 0, PhotoUID: "photoA"},
					{SlotIndex: 1, IsCaptionsSlot: true},
				},
			},
		}},
	}
	photos := map[string]photoImage{
		"photoA": {path: "/tmp/photoA.jpg", width: 2000, height: 3000},
	}
	// Empty caption map — no captions anywhere.
	data, _ := buildTemplateData(groups, photos, CaptionMap{}, DefaultLayoutConfig(), nil)

	page := data.Sections[0].Pages[0]
	if page.HasCaptions {
		t.Error("footer should still be suppressed even when captions slot has no captions")
	}
	if !page.Slots[1].HasCaptionsList {
		t.Error("slot 1 must still be flagged as captions slot")
	}
	if len(page.Slots[1].CaptionsList) != 0 {
		t.Errorf("expected empty CaptionsList, got %d", len(page.Slots[1].CaptionsList))
	}
}

// --- PageSlot helpers ---

func TestPageSlot_IsCaptions(t *testing.T) {
	t.Run("captions-only slot is captions", func(t *testing.T) {
		s := database.PageSlot{IsCaptionsSlot: true}
		if !s.IsCaptions() {
			t.Error("expected IsCaptions=true")
		}
		if s.IsEmpty() {
			t.Error("captions slot must not be considered empty")
		}
		if s.IsTextSlot() {
			t.Error("captions slot must not be considered a text slot")
		}
	})
	t.Run("empty slot is empty", func(t *testing.T) {
		s := database.PageSlot{}
		if !s.IsEmpty() {
			t.Error("expected empty slot to be IsEmpty=true")
		}
		if s.IsCaptions() {
			t.Error("empty slot must not be IsCaptions")
		}
	})
}

// --- renderSlotCaptionLatex ---

func TestRenderSlotCaptionLatex(t *testing.T) {
	t.Run("badge plus caption text", func(t *testing.T) {
		fc := FooterCaption{Markers: []int{2}, Caption: "Hello", ChapterColor: "8B0000"}
		got := renderSlotCaptionLatex(fc, 4.0)
		want := `\captionbadge{4.00}{6.00}{8B0000}{white}{2}\hspace{1.50mm}Hello`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("no badge for single-photo page", func(t *testing.T) {
		fc := FooterCaption{Caption: "Only one photo"}
		got := renderSlotCaptionLatex(fc, 4.0)
		want := `Only one photo`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("embedded newline becomes hard break", func(t *testing.T) {
		fc := FooterCaption{Markers: []int{1}, Caption: "Line 1\nLine 2"}
		got := renderSlotCaptionLatex(fc, 4.0)
		if !strings.Contains(got, `Line 1\\Line 2`) {
			t.Errorf("expected hard break in %q", got)
		}
	})

	t.Run("trailing newline is dropped (no dangling break)", func(t *testing.T) {
		fc := FooterCaption{Markers: []int{1}, Caption: "Caption\n"}
		got := renderSlotCaptionLatex(fc, 4.0)
		if strings.HasSuffix(got, `\\`) {
			t.Errorf("did not expect trailing \\\\, got: %q", got)
		}
	})
}

func TestSlotCaptionIndentMM(t *testing.T) {
	// Defaults: 4mm badge + 1.5mm gap = 5.5mm.
	if got := slotCaptionIndentMM(4.0); got != 5.5 {
		t.Errorf("default badge: got %v, want 5.5", got)
	}
	// Custom 6mm badge + 1.5mm gap = 7.5mm.
	if got := slotCaptionIndentMM(6.0); got != 7.5 {
		t.Errorf("custom badge: got %v, want 7.5", got)
	}
}
