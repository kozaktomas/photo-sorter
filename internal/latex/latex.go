package latex

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

//go:embed templates/book.tex
var templateFS embed.FS

// --- Export Report Types ---

// ProgressInfo is reported by GeneratePDFWithCallbacks to track export phases.
// Phase is one of: "downloading_photos", "compiling_pass1", "compiling_pass2".
// Current/Total are phase-local (e.g. downloaded photo count). PhotoUID is
// populated only for downloading_photos events.
type ProgressInfo struct {
	Phase    string
	Current  int
	Total    int
	PhotoUID string
}

// ExportOptions are optional parameters for GeneratePDFWithCallbacks.
type ExportOptions struct {
	Debug      bool
	OnProgress func(ProgressInfo)
}

// ExportReport contains metadata about a PDF export for quality analysis.
type ExportReport struct {
	BookTitle  string       `json:"book_title"`
	PageCount  int          `json:"page_count"`
	PhotoCount int          `json:"photo_count"`
	Pages      []ReportPage `json:"pages"`
	Warnings   []string     `json:"warnings"`
}

// ReportPage describes a single page in the export report.
type ReportPage struct {
	PageNumber int           `json:"page_number"`
	Format     string        `json:"format"`
	Title      string        `json:"title,omitempty"`
	IsDivider  bool          `json:"is_divider"`
	Photos     []ReportPhoto `json:"photos,omitempty"`
}

// ReportPhoto describes a single photo placement in the export report.
type ReportPhoto struct {
	PhotoUID     string  `json:"photo_uid"`
	SlotIndex    int     `json:"slot_index"`
	EffectiveDPI float64 `json:"effective_dpi"`
	LowRes       bool    `json:"low_res"`
}

// --- Template Types ---

// FooterCaption holds a numbered caption for the footer zone.
// Markers is the slice of marker numbers attached to this caption — one badge
// is rendered per marker (e.g. three photos sharing the same caption produce
// three side-by-side badges, not a merged "1–3" range).
// ChapterColor is the hex color (without #) used to style the marker badge;
// empty string means no chapter color (badge falls back to black at 55% opacity).
type FooterCaption struct {
	Markers      []int
	Caption      string
	ChapterColor string
}

// TemplateSlot holds pre-computed TikZ coordinates for one photo, text, or
// captions slot.
type TemplateSlot struct {
	HasPhoto        bool
	HasText         bool
	HasCaptionsList bool
	// Clip rectangle (mm from page bottom-left — TikZ convention).
	ClipX, ClipY float64
	ClipW, ClipH float64
	// Image node anchor position.
	ImgX, ImgY float64
	// Sizing dimension and value.
	SizeDim  string  // "width" or "height"
	SizeVal  float64 // mm
	FilePath string
	// DPI tracking.
	EffectiveDPI float64
	// Archival mode.
	IsArchival bool
	MatInsetMM float64
	// Border rect (for archival — same as clip for modern).
	BorderX, BorderY, BorderW, BorderH float64
	// Text type: "T1", "T2", "T3".
	TextContent  string
	TextType     string
	ChapterColor string // hex color e.g. "8B0000" (without #), empty = no color
	// CaptionsList holds the footer-caption entries routed into this slot
	// when the slot is the page's captions slot. The LaTeX template renders
	// them stacked vertically; the page's bottom captions strip is suppressed.
	CaptionsList []FooterCaption
	// Text padding (mm) for text slots adjacent to photos in mixed layouts.
	TextPadLeft  float64
	TextPadRight float64
	// H1 bleed (mm) — how far the colored heading box extends beyond linewidth on each side.
	// Bleed towards the page edge (outside), not towards the gutter (adjacent photo column).
	BleedLeftMM  float64
	BleedRightMM float64
	// RaggedRight: use left-aligned text instead of justified (for narrow columns).
	RaggedRight bool
	// Caption marker (1-based; 0 = no marker).
	CaptionMarker        int
	CaptionMarkerX       float64 // bottom-left X of marker rect
	CaptionMarkerY       float64 // bottom-left Y of marker rect
	CaptionMarkerCenterX float64 // center X for number node
	CaptionMarkerCenterY float64 // center Y for number node
}

// TemplatePage holds slots for a single page.
type TemplatePage struct {
	Slots          []TemplateSlot
	IsLast         bool
	PageNumber     int    // continuous page number (1-based)
	IsRecto        bool   // true for odd pages (right-hand, recto)
	Style          string // "modern" or "archival"
	HidePageNumber bool   // suppress folio rendering on this page (numbering continues)
	// Content area bounds.
	ContentLeftX  float64
	ContentRightX float64
	ContentW      float64
	// Clip area bounds — expanded by heading bleed so colored heading boxes
	// are visible in the margins. Photos are still constrained by slot-level clips.
	ClipLeftX  float64
	ClipRightX float64
	// Header zone.
	HeaderY float64 // Y position for running header text
	// Canvas zone.
	CanvasTopY    float64
	CanvasBottomY float64
	// Footer zone.
	FooterRuleY   float64 // Y of 0.3pt separation line
	FolioX        float64
	FolioY        float64
	FolioAnchor   string // "south east" (recto) or "south west" (verso)
	Captions      []FooterCaption
	CaptionBlockX float64
	CaptionBlockY float64
	CaptionBlockW float64
	HasCaptions   bool
}

// TemplateSection holds a section title and its pages.
type TemplateSection struct {
	Title string
	Pages []TemplatePage
}

// TemplateData is the root data passed to the LaTeX template.
type TemplateData struct {
	Sections        []TemplateSection
	PageW           float64
	PageH           float64
	DebugOverlay    bool
	DebugColOffsets []float64 // relative X offsets for column left edges

	// Typography settings (from per-book configuration).
	// BodyFontDeclaration / HeadingFontDeclaration are full LaTeX commands
	// (\setmainfont{...}[...] / \setsansfont{...}[...]) built by
	// FontEntry.LatexDeclaration. They are inserted verbatim into the
	// template preamble so per-font customization (e.g. file-based loading
	// for variable fonts that need explicit wght axis configuration) lives
	// in Go rather than the template.
	BodyFontDeclaration    string
	HeadingFontDeclaration string
	BodyFontSize           float64 // e.g. 11.0
	BodyLineHeight         float64 // e.g. 15.0
	H1FontSize             float64 // e.g. 18.0
	H1Leading              float64 // e.g. 22.0
	H2FontSize             float64 // e.g. 13.0
	H2Leading              float64 // e.g. 16.0
	CaptionOpacity         int     // 0-100 for LaTeX black!N notation
	CaptionFontSize        float64 // e.g. 9.0
	CaptionLeading         float64 // e.g. 11.0
	CaptionBadgeSize       float64 // e.g. 4.0 (mm) — square dimension of footer caption badges
}

// photoImage holds downloaded photo data for dimension lookup.
type photoImage struct {
	path   string
	width  int
	height int
}

// sectionGroup groups pages belonging to the same section.
type sectionGroup struct {
	sectionID    string
	title        string
	chapterColor string // hex color without # (e.g. "8B0000"), empty = no color
	pages        []database.BookPage
}

// CaptionMap is a nested map: sectionID -> photoUID -> caption text.
type CaptionMap map[string]map[string]string

// pageBuilder tracks state while building pages across sections.
type pageBuilder struct {
	config            LayoutConfig
	photos            map[string]photoImage
	captions          CaptionMap
	totalContentPages int
	contentPageIdx    int
	pageNumber        int
	photoSet          map[string]bool
	reportPages       []ReportPage
	headingColorBleed float64
}

const (
	downloadConcurrency = 5
	lowResDPIThreshold  = 200.0
	sizeDimHeight       = "height"
	sizeDimWidth        = "width"
)

// GeneratePDF renders a photo book to PDF using lualatex.
func GeneratePDF(
	ctx context.Context, pp *photoprism.PhotoPrism, br database.BookReader, bookID string,
) ([]byte, *ExportReport, error) {
	return GeneratePDFWithCallbacks(ctx, pp, br, bookID, ExportOptions{})
}

// GeneratePDFWithOptions renders a photo book to PDF with optional debug overlay.
func GeneratePDFWithOptions(
	ctx context.Context, pp *photoprism.PhotoPrism,
	br database.BookReader, bookID string, debug bool,
) ([]byte, *ExportReport, error) {
	return GeneratePDFWithCallbacks(ctx, pp, br, bookID, ExportOptions{Debug: debug})
}

// GeneratePDFWithCallbacks renders a photo book to PDF, emitting progress via
// opts.OnProgress if non-nil. Phases: downloading_photos, compiling_pass1,
// compiling_pass2.
//
//nolint:cyclop // Orchestration function that fetches multiple resources.
func GeneratePDFWithCallbacks(
	ctx context.Context, pp *photoprism.PhotoPrism,
	br database.BookReader, bookID string, opts ExportOptions,
) ([]byte, *ExportReport, error) {
	book, err := br.GetBook(ctx, bookID)
	if err != nil || book == nil {
		return nil, nil, fmt.Errorf("book not found: %s", bookID)
	}

	sections, err := br.GetSections(ctx, bookID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get sections: %w", err)
	}

	chapters, err := br.GetChapters(ctx, bookID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get chapters: %w", err)
	}

	pages, err := br.GetPages(ctx, bookID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get pages: %w", err)
	}

	if len(pages) == 0 {
		return nil, nil, errors.New("book has no pages")
	}

	SortPagesBySectionOrder(pages, sections)
	captions := buildCaptionMap(ctx, br, sections)
	uidSet := collectPhotoUIDs(pages)

	tmpDir, err := os.MkdirTemp("", "book-pdf-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	photos := downloadPhotosWithProgress(ctx, pp, uidSet, tmpDir, opts.OnProgress)
	groups := groupPagesBySection(pages, sections, chapters)
	config := DefaultLayoutConfig()
	data, report := buildTemplateData(groups, photos, captions, config, book)

	if opts.Debug {
		applyDebugOverlay(&data, config)
	}

	// Layout validation.
	validationWarnings := ValidatePages(data.Sections, config)
	for _, vw := range validationWarnings {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Layout: page %d slot %d: %s", vw.PageNumber, vw.SlotIndex, vw.Message))
	}

	addDPIWarnings(report)

	pdfData, err := compileLatexWithProgress(ctx, data, tmpDir, opts.OnProgress)
	if err != nil {
		return nil, nil, err
	}
	return pdfData, report, nil
}

// collectPhotoUIDs extracts unique photo UIDs from all page slots.
func collectPhotoUIDs(pages []database.BookPage) map[string]bool {
	uidSet := make(map[string]bool)
	for _, p := range pages {
		for _, s := range p.Slots {
			if s.PhotoUID != "" {
				uidSet[s.PhotoUID] = true
			}
		}
	}
	return uidSet
}

// addDPIWarnings scans report pages and adds warnings for low-res photos.
func addDPIWarnings(report *ExportReport) {
	for _, rp := range report.Pages {
		for _, photo := range rp.Photos {
			if photo.LowRes {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("Page %d, slot %d (%s): effective DPI %.0f is below %d",
						rp.PageNumber, photo.SlotIndex, photo.PhotoUID, photo.EffectiveDPI, int(lowResDPIThreshold)))
			}
		}
	}
}

// applyDebugOverlay enables the debug overlay and computes column offsets.
func applyDebugOverlay(data *TemplateData, config LayoutConfig) {
	data.DebugOverlay = true
	offsets := make([]float64, config.GridColumns)
	for c := range config.GridColumns {
		offsets[c] = config.ColOffset(c)
	}
	data.DebugColOffsets = offsets
}

// buildCaptionMap loads section photos and builds a nested caption lookup.
func buildCaptionMap(ctx context.Context, br database.BookReader, sections []database.BookSection) CaptionMap {
	captions := make(CaptionMap, len(sections))
	for _, s := range sections {
		photos, err := br.GetSectionPhotos(ctx, s.ID)
		if err != nil {
			log.Printf("WARNING: failed to load section photos for %s: %v", s.ID, err)
			continue
		}
		m := make(map[string]string, len(photos))
		for _, p := range photos {
			if p.Description != "" {
				m[p.PhotoUID] = p.Description
			}
		}
		if len(m) > 0 {
			captions[s.ID] = m
		}
	}
	return captions
}

// lookupCaption returns the caption for a photo in a specific section.
func lookupCaption(captions CaptionMap, sectionID, photoUID string) string {
	if sectionCaptions, ok := captions[sectionID]; ok {
		return sectionCaptions[photoUID]
	}
	return ""
}

// SortPagesBySectionOrder sorts pages by section order then sort_order.
func SortPagesBySectionOrder(pages []database.BookPage, sections []database.BookSection) {
	sectionOrder := make(map[string]int, len(sections))
	for i, s := range sections {
		sectionOrder[s.ID] = i
	}
	sort.SliceStable(pages, func(i, j int) bool {
		si := sectionOrder[pages[i].SectionID]
		sj := sectionOrder[pages[j].SectionID]
		if si != sj {
			return si < sj
		}
		return pages[i].SortOrder < pages[j].SortOrder
	})
}

// groupPagesBySection groups consecutive pages by their section ID.
func groupPagesBySection(
	pages []database.BookPage, sections []database.BookSection, chapters []database.BookChapter,
) []sectionGroup {
	sectionTitles := make(map[string]string, len(sections))
	sectionChapters := make(map[string]string, len(sections))
	for _, s := range sections {
		sectionTitles[s.ID] = s.Title
		sectionChapters[s.ID] = s.ChapterID
	}

	chapterColors := make(map[string]string, len(chapters))
	for _, c := range chapters {
		if c.Color != "" {
			chapterColors[c.ID] = strings.TrimPrefix(c.Color, "#")
		}
	}

	var groups []sectionGroup
	lastSectionID := ""
	for _, p := range pages {
		if p.SectionID != lastSectionID {
			groups = append(groups, sectionGroup{
				sectionID:    p.SectionID,
				title:        sectionTitles[p.SectionID],
				chapterColor: chapterColors[sectionChapters[p.SectionID]],
				pages:        []database.BookPage{p},
			})
			lastSectionID = p.SectionID
		} else {
			groups[len(groups)-1].pages = append(groups[len(groups)-1].pages, p)
		}
	}
	return groups
}

// buildTemplateData constructs the template data and export report.
func buildTemplateData(
	groups []sectionGroup, photos map[string]photoImage,
	captions CaptionMap, config LayoutConfig, book *database.PhotoBook,
) (TemplateData, *ExportReport) {
	// Resolve typography first so pageBuilder has the heading bleed value.
	typo := resolveBookTypography(book)

	pb := &pageBuilder{
		config:            config,
		photos:            photos,
		captions:          captions,
		photoSet:          make(map[string]bool),
		headingColorBleed: typo.headingColorBleed,
	}
	for _, g := range groups {
		pb.totalContentPages += len(g.pages)
	}

	tmplSections := make([]TemplateSection, 0, len(groups))
	for _, g := range groups {
		tmplSections = append(tmplSections, pb.buildSection(g))
	}

	bookTitle := ""
	if book != nil {
		bookTitle = book.Title
	}

	return TemplateData{
			Sections:               tmplSections,
			PageW:                  PageW,
			PageH:                  PageH,
			BodyFontDeclaration:    typo.bodyFontDeclaration,
			HeadingFontDeclaration: typo.headingFontDeclaration,
			BodyFontSize:           typo.bodyFontSize,
			BodyLineHeight:         typo.bodyLineHeight,
			H1FontSize:             typo.h1FontSize,
			H1Leading:              typo.h1Leading,
			H2FontSize:             typo.h2FontSize,
			H2Leading:              typo.h2Leading,
			CaptionOpacity:         typo.captionOpacity,
			CaptionFontSize:        typo.captionFontSize,
			CaptionLeading:         typo.captionLeading,
			CaptionBadgeSize:       typo.captionBadgeSize,
		}, &ExportReport{
			BookTitle:  bookTitle,
			PageCount:  pb.pageNumber,
			PhotoCount: len(pb.photoSet),
			Pages:      pb.reportPages,
		}
}

// resolvedTypography holds resolved font/size values with defaults applied.
type resolvedTypography struct {
	bodyFontDeclaration    string
	headingFontDeclaration string
	bodyFontSize           float64
	bodyLineHeight         float64
	h1FontSize             float64
	h1Leading              float64
	h2FontSize             float64
	h2Leading              float64
	captionOpacity         int
	captionFontSize        float64
	captionLeading         float64
	captionBadgeSize       float64
	headingColorBleed      float64
}

// resolveBookTypography resolves typography settings from a PhotoBook with fallbacks.
func resolveBookTypography(book *database.PhotoBook) resolvedTypography {
	fontRoot := FindFontRoot()
	defaultBody, _ := GetFont(DefaultBodyFont)
	defaultHeading, _ := GetFont(DefaultHeadingFont)
	rt := resolvedTypography{
		bodyFontDeclaration:    defaultBody.LatexDeclaration(`\setmainfont`, fontRoot),
		headingFontDeclaration: defaultHeading.LatexDeclaration(`\setsansfont`, fontRoot),
		bodyFontSize:           DefaultBodyFontSize,
		bodyLineHeight:         DefaultBodyLineHeight,
		h1FontSize:             DefaultH1FontSize,
		h2FontSize:             DefaultH2FontSize,
		captionOpacity:         int(DefaultCaptionOpacity * 100),
		captionFontSize:        DefaultCaptionFontSize,
		captionBadgeSize:       DefaultCaptionBadgeSize,
		headingColorBleed:      DefaultHeadingColorBleed,
	}

	if book != nil {
		applyBookFonts(&rt, book, fontRoot)
		applyBookSizes(&rt, book)
	}

	rt.h1Leading = math.Ceil(rt.h1FontSize * 1.22)
	rt.h2Leading = math.Ceil(rt.h2FontSize * 1.23)
	rt.captionLeading = math.Ceil(rt.captionFontSize * 1.22)
	return rt
}

// applyBookFonts overrides font declarations from book settings when available.
func applyBookFonts(rt *resolvedTypography, book *database.PhotoBook, fontRoot string) {
	if f, ok := GetFont(book.BodyFont); ok && f.LatexName != "" {
		rt.bodyFontDeclaration = f.LatexDeclaration(`\setmainfont`, fontRoot)
	}
	if f, ok := GetFont(book.HeadingFont); ok && f.LatexName != "" {
		rt.headingFontDeclaration = f.LatexDeclaration(`\setsansfont`, fontRoot)
	}
}

// applyBookSizes overrides font sizes and opacity from book settings when set.
func applyBookSizes(rt *resolvedTypography, book *database.PhotoBook) {
	if book.BodyFontSize > 0 {
		rt.bodyFontSize = book.BodyFontSize
	}
	if book.BodyLineHeight > 0 {
		rt.bodyLineHeight = book.BodyLineHeight
	}
	if book.H1FontSize > 0 {
		rt.h1FontSize = book.H1FontSize
	}
	if book.H2FontSize > 0 {
		rt.h2FontSize = book.H2FontSize
	}
	if book.CaptionOpacity > 0 {
		rt.captionOpacity = int(book.CaptionOpacity * 100)
	}
	if book.CaptionFontSize > 0 {
		rt.captionFontSize = book.CaptionFontSize
	}
	if book.CaptionBadgeSize > 0 {
		rt.captionBadgeSize = book.CaptionBadgeSize
	}
	// heading_color_bleed is NOT NULL in the DB, and 0 is an explicit user
	// choice (no bleed — heading box stops at content edge), so always assign.
	rt.headingColorBleed = book.HeadingColorBleed
}

// buildSection builds a TemplateSection and accumulates report data. Every
// page from the section group is rendered, including pages with no photos
// and no text — they are preserved as blank pages so they keep a folio
// number and shift the pagination of pages that follow them.
func (pb *pageBuilder) buildSection(g sectionGroup) TemplateSection {
	tmplPages := make([]TemplatePage, 0, len(g.pages))
	for _, p := range g.pages {
		pb.contentPageIdx++
		pb.pageNumber++
		tmplPages = append(tmplPages, pb.buildContentPage(p, g.chapterColor))
	}

	return TemplateSection{
		Title: g.title,
		Pages: tmplPages,
	}
}

// computeZones returns the TikZ Y coordinates for the 3-zone layout.
// TikZ origin is page bottom-left, Y increases upward.
func (pb *pageBuilder) computeZones(isRecto bool) (
	contentLeftX, contentRightX, headerY, canvasTopY, canvasBottomY,
	footerRuleY, folioX, folioY float64, folioAnchor string,
) {
	cfg := pb.config
	// Mirrored margins: recto has inside (binding) on left, verso has inside on right.
	if isRecto {
		contentLeftX = cfg.InsideMarginMM
	} else {
		contentLeftX = cfg.OutsideMarginMM
	}
	contentW := cfg.ContentWidth()
	contentRightX = contentLeftX + contentW

	// Vertical zones (from top of page, converted to TikZ Y from bottom).
	topEdge := PageH - cfg.TopMarginMM              // 200mm from bottom
	headerY = topEdge - 2.0                         // baseline in header zone
	canvasTopY = topEdge - cfg.HeaderHeightMM       // 196mm
	canvasBottomY = canvasTopY - cfg.CanvasHeightMM // 24mm
	footerRuleY = canvasBottomY                     // 24mm

	// Folio at bottom margin, mirrored.
	folioY = cfg.BottomMarginMM / 2.0
	if isRecto {
		folioX = contentRightX
		folioAnchor = "south east"
	} else {
		folioX = contentLeftX
		folioAnchor = "south west"
	}
	return contentLeftX, contentRightX, headerY, canvasTopY, canvasBottomY, footerRuleY, folioX, folioY, folioAnchor
}

// buildContentPage builds a single TemplatePage with slots and accumulates report data.
func (pb *pageBuilder) buildContentPage(p database.BookPage, chapterColor string) TemplatePage {
	isLast := pb.contentPageIdx == pb.totalContentPages
	isRecto := pb.pageNumber%2 == 1
	style := p.Style
	if style == "" {
		style = "modern"
	}

	contentLeftX, contentRightX, headerY, canvasTopY,
		canvasBottomY, footerRuleY, folioX, folioY,
		folioAnchor := pb.computeZones(isRecto)
	contentW := pb.config.ContentWidth()

	// Build slots using grid system.
	slots := FormatSlotsGridWithSplit(p.Format, pb.config, p.SplitPosition)
	tmplSlots, reportPhotos, footerCaptions := pb.buildSlots(
		p, slots, contentLeftX, canvasTopY, style, isRecto, chapterColor,
	)

	// If the user marked a slot as the captions slot, route the footer
	// captions into that slot and suppress the bottom caption strip.
	tmplSlots, footerCaptions = routeCaptionsSlot(
		p, slots, tmplSlots, footerCaptions, contentLeftX, canvasTopY, chapterColor,
	)

	pb.reportPages = append(pb.reportPages, ReportPage{
		PageNumber: pb.pageNumber,
		Format:     p.Format,
		Title:      p.Description,
		Photos:     reportPhotos,
	})

	captionBlockX, captionBlockY, captionBlockW, hasCaptions :=
		captionBlockPosition(footerCaptions, contentLeftX, contentW, footerRuleY)

	// Expand clip bounds by heading bleed so colored heading boxes
	// extend into the margins. Photos stay constrained by slot-level clips.
	bleed := pb.headingColorBleed
	clipLeftX := max(0, contentLeftX-bleed)
	clipRightX := min(PageW, contentRightX+bleed)

	return TemplatePage{
		Slots:          tmplSlots,
		IsLast:         isLast,
		PageNumber:     pb.pageNumber,
		IsRecto:        isRecto,
		Style:          style,
		HidePageNumber: p.HidePageNumber,
		ContentLeftX:   contentLeftX,
		ContentRightX:  contentRightX,
		ContentW:       contentW,
		ClipLeftX:      clipLeftX,
		ClipRightX:     clipRightX,
		HeaderY:        headerY,
		CanvasTopY:     canvasTopY,
		CanvasBottomY:  canvasBottomY,
		FooterRuleY:    footerRuleY,
		FolioX:         folioX,
		FolioY:         folioY,
		FolioAnchor:    folioAnchor,
		Captions:       footerCaptions,
		CaptionBlockX:  captionBlockX,
		CaptionBlockY:  captionBlockY,
		CaptionBlockW:  captionBlockW,
		HasCaptions:    hasCaptions,
	}
}

// captionBlockPosition computes the footer caption block placement. The
// block is anchored 1mm below the canvas bottom (anchor=north, so this is
// the TOP of the text); captions then grow downward toward the folio. With
// this position, a single-line caption keeps ~8mm of clearance to the
// folio, while a two-line caption still leaves ~5mm — enough breathing
// room so the folio never feels glued to the bottom line. Three-line
// captions are tight; if they become common, consider shrinking the
// caption font instead of stealing from the folio gap.
//
// Returns zero-valued coordinates and hasCaptions=false when there are no
// captions to render.
func captionBlockPosition(
	captions []FooterCaption, contentLeftX, contentW, footerRuleY float64,
) (x, y, w float64, hasCaptions bool) {
	if len(captions) == 0 {
		return 0, 0, 0, false
	}
	return contentLeftX, footerRuleY - 1.0, contentW, true
}

// slotCaption pairs a slot index with its caption text.
type slotCaption struct {
	slotIdx int
	caption string
}

// captionTracking holds precomputed caption and marker data for slot building.
type captionTracking struct {
	markerMap map[int]int // slotIdx -> marker number (1-based)
	captions  []slotCaption
}

// buildCaptionTracking collects captions and assigns marker numbers for a page.
func buildCaptionTracking(p database.BookPage, slots []SlotRect, captions CaptionMap) captionTracking {
	photoCount := 0
	for i := range slots {
		if getPageSlot(p, i).PhotoUID != "" {
			photoCount++
		}
	}

	var captionList []slotCaption
	for i := range slots {
		ps := getPageSlot(p, i)
		if ps.PhotoUID != "" {
			if caption := lookupCaption(captions, p.SectionID, ps.PhotoUID); caption != "" {
				captionList = append(captionList, slotCaption{slotIdx: i, caption: caption})
			}
		}
	}

	markerMap := make(map[int]int)
	if photoCount > 1 && len(captionList) > 0 {
		for idx, sc := range captionList {
			markerMap[sc.slotIdx] = idx + 1
		}
	}

	return captionTracking{markerMap: markerMap, captions: captionList}
}

// placeCaptionMarker positions a numbered caption marker on a photo slot.
func placeCaptionMarker(ts *TemplateSlot, markerNum int, cfg LayoutConfig, isRecto bool) {
	ts.CaptionMarker = markerNum
	if markerNum <= 0 {
		return
	}
	markerSize := cfg.BaselineUnitMM
	markerInset := markerSize / 2.0
	if isRecto {
		ts.CaptionMarkerX = ts.ClipX + ts.ClipW - markerInset - markerSize
	} else {
		ts.CaptionMarkerX = ts.ClipX + markerInset
	}
	ts.CaptionMarkerY = ts.ClipY + ts.ClipH - markerInset - markerSize
	ts.CaptionMarkerCenterX = ts.CaptionMarkerX + markerSize/2
	ts.CaptionMarkerCenterY = ts.CaptionMarkerY + markerSize/2
}

// buildFooterCaptions creates footer caption entries from caption tracking data.
// chapterColor is propagated to every caption so the template can render the
// marker as a colored badge matching the photo overlay badge style.
func buildFooterCaptions(ct captionTracking, chapterColor string) []FooterCaption {
	footerCaptions := make([]FooterCaption, 0, len(ct.captions))
	for _, sc := range ct.captions {
		marker := ct.markerMap[sc.slotIdx]
		var markers []int
		if marker > 0 {
			markers = []int{marker}
		}
		footerCaptions = append(footerCaptions, FooterCaption{
			Markers:      markers,
			Caption:      sc.caption,
			ChapterColor: chapterColor,
		})
	}
	return mergeFooterCaptions(footerCaptions)
}

// mergeFooterCaptions groups footer captions by identical text. Each photo's
// marker remains a separate badge — the template renders one badge per entry
// in Markers — but identical caption text only appears once.
// Order is preserved by first occurrence.
func mergeFooterCaptions(caps []FooterCaption) []FooterCaption {
	if len(caps) <= 1 {
		return caps
	}

	var groups []FooterCaption
	seen := make(map[string]int) // caption text -> index in groups

	for _, c := range caps {
		idx, ok := seen[c.Caption]
		if ok {
			groups[idx].Markers = append(groups[idx].Markers, c.Markers...)
		} else {
			seen[c.Caption] = len(groups)
			groups = append(groups, FooterCaption{
				Markers:      append([]int(nil), c.Markers...),
				Caption:      c.Caption,
				ChapterColor: c.ChapterColor,
			})
		}
	}

	for i := range groups {
		sort.Ints(groups[i].Markers)
	}
	return groups
}

// buildSlots builds template slots, report photos, and footer captions for a page.
func (pb *pageBuilder) buildSlots(
	p database.BookPage, slots []SlotRect,
	contentLeftX, canvasTopY float64, style string, isRecto bool, chapterColor string,
) ([]TemplateSlot, []ReportPhoto, []FooterCaption) {
	isArchival := style == "archival"
	cfg := pb.config
	ct := buildCaptionTracking(p, slots, pb.captions)

	tmplSlots := make([]TemplateSlot, len(slots))
	var reportPhotos []ReportPhoto

	for i, slot := range slots {
		ps := getPageSlot(p, i)

		if ps.IsTextSlot() {
			ts := buildTextSlotNew(slot, ps, contentLeftX, canvasTopY)
			ts.ChapterColor = chapterColor
			tmplSlots[i] = ts
			continue
		}

		uid := ps.PhotoUID
		if uid == "" {
			continue
		}

		img, ok := pb.photos[uid]
		if !ok {
			continue
		}

		cropScale := ps.CropScale
		if cropScale <= 0 {
			cropScale = 1.0
		}
		ts := buildPhotoSlotNew(
			slot, img, contentLeftX, canvasTopY, isArchival,
			cfg.ArchivalInsetMM, ps.CropX, ps.CropY, cropScale,
		)
		placeCaptionMarker(&ts, ct.markerMap[i], cfg, isRecto)
		ts.ChapterColor = chapterColor
		tmplSlots[i] = ts

		pb.photoSet[uid] = true
		reportPhotos = append(reportPhotos, ReportPhoto{
			PhotoUID:     uid,
			SlotIndex:    i,
			EffectiveDPI: ts.EffectiveDPI,
			LowRes:       ts.EffectiveDPI > 0 && ts.EffectiveDPI < lowResDPIThreshold,
		})
	}

	applyTextSlotPadding(tmplSlots, p.Format, pb.headingColorBleed)

	return tmplSlots, reportPhotos, buildFooterCaptions(ct, chapterColor)
}

// isSlotOnLeftEdge returns true if the slot touches the left page margin.
func isSlotOnLeftEdge(format string, slotIndex int) bool {
	switch format {
	case FormatFullscreen:
		return true
	case Format2Portrait:
		return slotIndex == 0
	case Format4Landscape:
		return slotIndex == 0 || slotIndex == 2
	case Format1P2L:
		return slotIndex == 0
	case Format2L1P:
		return slotIndex == 0 || slotIndex == 1
	default:
		return true
	}
}

// isSlotOnRightEdge returns true if the slot touches the right page margin.
func isSlotOnRightEdge(format string, slotIndex int) bool {
	switch format {
	case FormatFullscreen:
		return true
	case Format2Portrait:
		return slotIndex == 1
	case Format4Landscape:
		return slotIndex == 1 || slotIndex == 3
	case Format1P2L:
		return slotIndex == 1 || slotIndex == 2
	case Format2L1P:
		return slotIndex == 2
	default:
		return true
	}
}

// applyTextSlotPadding configures heading bleed on text slots. The heading
// color box bleeds outward only on page-margin edges — on interior sides
// (adjacent to another slot) it stops at the slot boundary so it doesn't
// collide with the neighbouring photo. Body text fills the full slot width,
// relying on the column gutter between slots for breathing room, which
// keeps its right/left edge aligned with adjacent photo edges.
func applyTextSlotPadding(slots []TemplateSlot, format string, headingBleed float64) {
	for i := range slots {
		if !slots[i].HasText {
			continue
		}
		if isSlotOnLeftEdge(format, i) {
			slots[i].BleedLeftMM = headingBleed
		}
		if isSlotOnRightEdge(format, i) {
			slots[i].BleedRightMM = headingBleed
		}
	}
}

// latexEscapeRaw escapes special LaTeX characters in user text.
func latexEscapeRaw(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`{`, `\{`,
		`}`, `\}`,
		`%`, `\%`,
		`&`, `\&`,
		`#`, `\#`,
		`$`, `\$`,
		`_`, `\_`,
		`^`, `\textasciicircum{}`,
		`~`, `\textasciitilde{}`,
	)
	return replacer.Replace(s)
}

// czechTypographyRe matches single-letter Czech prepositions followed by a space.
var czechTypographyRe = regexp.MustCompile(`(^|[\s])([vVkKsSzZuUoOiIaA])\s`)

// czechTypography inserts LaTeX non-breaking spaces (~) after single-letter Czech.
// prepositions to prevent them from appearing at end of line.
func czechTypography(s string) string {
	return czechTypographyRe.ReplaceAllString(s, "${1}${2}~")
}

// latexEscape escapes special LaTeX characters and applies Czech typography rules.
func latexEscape(s string) string {
	return czechTypography(latexEscapeRaw(s))
}

// latexEscapeCaptionRaw escapes special LaTeX characters like latexEscapeRaw,
// but preserves `~` so it can be used as a LaTeX non-breaking space in captions.
func latexEscapeCaptionRaw(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`{`, `\{`,
		`}`, `\}`,
		`%`, `\%`,
		`&`, `\&`,
		`#`, `\#`,
		`$`, `\$`,
		`_`, `\_`,
		`^`, `\textasciicircum{}`,
	)
	return replacer.Replace(s)
}

// latexEscapeCaption escapes caption text for LaTeX. Unlike latexEscape, it
// supports three lightweight inline formatting features: **bold**, *italic*,
// and `~` as a non-breaking space. No other markdown syntax (headings, lists,
// blockquotes, tables) is recognized. Czech typography rules still apply.
func latexEscapeCaption(s string) string {
	escaped := latexEscapeCaptionRaw(s)
	// Bold before italic so `**text**` is matched before the inner `*text*`.
	escaped = boldRe.ReplaceAllString(escaped, `\textbf{$1}`)
	escaped = italicRe.ReplaceAllString(escaped, `\textit{$1}`)
	return czechTypography(escaped)
}

// bookTemplateFuncMap returns the template.FuncMap used by book.tex. It is
// shared between compileLatex and tests so the rendering paths exercised by
// tests stay in sync with production output.
func bookTemplateFuncMap(typoConfig TypographyConfig) template.FuncMap {
	return template.FuncMap{
		"latexEscape":     latexEscape,
		"markdownToLatex": MarkdownToLatex,
		"markdownToLatexColor": func(md, color string, bleedL, bleedR float64) string {
			return MarkdownToLatexWithTypography(md, color, bleedL, bleedR, typoConfig)
		},
		"contrastTextColor":   contrastTextColorOrWhite,
		"renderFooterCaption": renderFooterCaptionsLatex,
		"renderSlotCaption":   renderSlotCaptionLatex,
		"slotCaptionIndentMM": slotCaptionIndentMM,
		"addFloat":            func(a, b float64) float64 { return a + b },
		"subtractFloat":       func(a, b float64) float64 { return a - b },
		"mulFloat":            func(a, b float64) float64 { return a * b },
		"divFloat":            func(a, b float64) float64 { return a / b },
	}
}

// renderFooterCaptionsLatex builds the full LaTeX content of the footer caption
// block from the page's captions. The returned string sits between the outer
// {...} braces of the tikz \node containing it.
//
// Each caption (badges + text) is wrapped in \mbox{} so it never breaks
// mid-sentence. Captions are joined by \quad — the only valid line break point
// in the paragraph. Newlines inside a caption's text become hard line breaks
// (\\) that split the caption into multiple \mbox segments stacked vertically;
// only the first segment carries the badges. A trailing newline at the end of
// a caption text forces a hard break before the next caption (no leading
// \quad on the new line) so users can author the exact wrap they want.
func renderFooterCaptionsLatex(caps []FooterCaption, badgeSize float64) string {
	if len(caps) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range caps {
		if i > 0 {
			// Skip the inter-caption \quad if the previous caption ended with
			// a hard break — otherwise the new line would start with a 1em
			// indent.
			if !strings.HasSuffix(caps[i-1].Caption, "\n") {
				b.WriteString(`\quad `)
			}
		}
		writeFooterCaption(&b, c, badgeSize)
	}
	return b.String()
}

// slotCaptionBadgeGapMM is the fixed horizontal gap (in mm) between the badge
// column and the caption text in a captions slot. It MUST stay in sync with
// the \hangindent value computed by slotCaptionIndentMM so that wrapped lines
// align under the first text character on line 1.
const slotCaptionBadgeGapMM = 1.5

// slotCaptionIndentMM is the hangindent value (mm) for a captions slot
// paragraph: badge width + fixed gap. It is exposed to the LaTeX template via
// the slotCaptionIndentMM template func and used in renderSlotCaptionLatex.
func slotCaptionIndentMM(badgeSize float64) float64 {
	return badgeSize + slotCaptionBadgeGapMM
}

// renderSlotCaptionLatex builds LaTeX for a single caption inside a captions
// slot: badges + fixed-width gap + escaped caption text. Unlike
// renderFooterCaptionsLatex it emits no \quad glue and no \mbox wrapper, so
// the surrounding parbox can wrap long caption text naturally across multiple
// lines. The fixed \hspace gap (slotCaptionBadgeGapMM) ensures wrapped lines
// align under the first text character via \hangindent set in the template.
// Embedded newlines in the caption text become hard line breaks (\\) inside
// the caption.
func renderSlotCaptionLatex(c FooterCaption, badgeSize float64) string {
	var b strings.Builder
	badges := buildCaptionBadges(c, badgeSize)
	b.WriteString(badges)
	// Split on newlines so authored line breaks inside a caption become hard
	// breaks in the output. Empty trailing segments are dropped so we don't
	// emit a bare \\ at the end.
	segments := strings.Split(c.Caption, "\n")
	for i, seg := range segments {
		if seg == "" && i == len(segments)-1 {
			continue
		}
		if i == 0 {
			if len(c.Markers) > 0 && seg != "" {
				fmt.Fprintf(&b, `\hspace{%.2fmm}`, slotCaptionBadgeGapMM)
			}
			b.WriteString(latexEscapeCaption(seg))
		} else {
			b.WriteString(`\\`)
			b.WriteString(latexEscapeCaption(seg))
		}
	}
	return b.String()
}

// writeFooterCaption renders one caption (badges + text, with optional inline
// hard breaks) into b. The caller is responsible for inter-caption separators.
func writeFooterCaption(b *strings.Builder, c FooterCaption, badgeSize float64) {
	badges := buildCaptionBadges(c, badgeSize)
	// Split text on newlines. The first segment carries the badges; subsequent
	// segments are plain mboxes. A trailing newline produces an empty final
	// segment which becomes a bare \\ (no empty mbox). Consecutive empty
	// segments (from multiple newlines) are collapsed to avoid emitting
	// multiple \\ in a row which causes "There's no line here to end".
	rawSegments := strings.Split(c.Caption, "\n")
	segments := make([]string, 0, len(rawSegments))
	prevEmpty := false
	for _, seg := range rawSegments {
		empty := seg == ""
		if empty && prevEmpty {
			continue // collapse consecutive empty segments
		}
		segments = append(segments, seg)
		prevEmpty = empty
	}
	for i, seg := range segments {
		switch {
		case i == 0:
			b.WriteString(`\mbox{`)
			b.WriteString(badges)
			if len(c.Markers) > 0 && seg != "" {
				b.WriteString(`\, `)
			}
			b.WriteString(latexEscapeCaption(seg))
			b.WriteString(`}`)
		case seg == "":
			// Trailing newline: emit a hard break with no follow-up mbox.
			b.WriteString(`\\`)
		default:
			b.WriteString(`\\\mbox{`)
			b.WriteString(latexEscapeCaption(seg))
			b.WriteString(`}`)
		}
	}
}

// buildCaptionBadges renders the marker badges for one caption (one badge per
// marker, separated by \thinspace).
func buildCaptionBadges(c FooterCaption, badgeSize float64) string {
	if len(c.Markers) == 0 {
		return ""
	}
	fontSize := badgeSize * 1.5
	var b strings.Builder
	for i, m := range c.Markers {
		if i > 0 {
			b.WriteString(`\thinspace `)
		}
		if c.ChapterColor != "" {
			fmt.Fprintf(&b, `\captionbadge{%.2f}{%.2f}{%s}{%s}{%d}`,
				badgeSize, fontSize, c.ChapterColor, contrastTextColorOrWhite(c.ChapterColor), m)
		} else {
			fmt.Fprintf(&b, `\captionbadgefb{%.2f}{%.2f}{%d}`,
				badgeSize, fontSize, m)
		}
	}
	return b.String()
}

// contrastTextColorOrWhite returns the LaTeX color name (white or black) that
// gives the best contrast against the given background hex. Empty hex (no
// chapter color) defaults to white because the fallback marker background is
// dark (black at 55% opacity).
func contrastTextColorOrWhite(hexColor string) string {
	if hexColor == "" {
		return "white"
	}
	return contrastTextColorLatex(hexColor)
}

// compileLatex writes the template and runs lualatex, returning the PDF bytes.
func compileLatex(ctx context.Context, data TemplateData, tmpDir string) ([]byte, error) {
	return compileLatexWithProgress(ctx, data, tmpDir, nil)
}

// compileLatexWithProgress writes the template and runs lualatex twice,
// reporting phase transitions to onProgress (which may be nil).
func compileLatexWithProgress(
	ctx context.Context, data TemplateData, tmpDir string, onProgress func(ProgressInfo),
) ([]byte, error) {
	typoConfig := TypographyConfig{
		H1Size:    data.H1FontSize,
		H1Leading: data.H1Leading,
		H2Size:    data.H2FontSize,
		H2Leading: data.H2Leading,
	}
	funcMap := bookTemplateFuncMap(typoConfig)
	tmpl, err := template.New("book.tex").Funcs(funcMap).ParseFS(templateFS, "templates/book.tex")
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	texPath := filepath.Join(tmpDir, "book.tex")
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	if err := os.WriteFile(texPath, buf.Bytes(), 0600); err != nil {
		return nil, fmt.Errorf("failed to write tex file: %w", err)
	}
	// Run lualatex twice — second pass resolves remember picture positions.
	// Arguments are safe (tmpDir from os.MkdirTemp, texPath derived from it).
	//
	// HOME is set to tmpDir so luaotfload writes its per-run cache there
	// (prevents "permission denied" from a read-only HOME). TEXMFCACHE and
	// TEXMFVAR are NOT overridden — they inherit from the process environment
	// so luaotfload finds the pre-built font database (Docker sets these to
	// /var/cache/luatex-cache; dev environments use the system default).
	// The font database must exist; without it, bracket-file font lookups
	// fail. Docker pre-generates it via luaotfload-tool --update; dev
	// environments get it from make install-fonts + luaotfload-tool.
	latexEnv := setEnvVars(os.Environ(), map[string]string{
		"HOME": tmpDir,
	})
	passPhases := [2]string{"compiling_pass1", "compiling_pass2"}
	for pass := range 2 {
		if onProgress != nil {
			onProgress(ProgressInfo{Phase: passPhases[pass]})
		}
		cmd := exec.CommandContext(ctx, "lualatex", //nolint:gosec
			"-interaction=nonstopmode",
			"-output-directory="+tmpDir,
			texPath,
		)
		cmd.Dir = tmpDir
		cmd.Env = latexEnv
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("lualatex pass %d failed: %w\n%s", pass+1, err, string(output))
		}
	}

	pdfPath := filepath.Join(tmpDir, "book.pdf")
	pdfData, err := os.ReadFile(pdfPath) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF: %w", err)
	}

	return pdfData, nil
}

// getPageSlot returns the full PageSlot for a slot index, or an empty slot.
func getPageSlot(page database.BookPage, slotIndex int) database.PageSlot {
	for _, s := range page.Slots {
		if s.SlotIndex == slotIndex {
			return s
		}
	}
	return database.PageSlot{SlotIndex: slotIndex}
}

// downloadPhotos fetches photos concurrently and returns a map of UID -> photoImage.
func downloadPhotos(
	ctx context.Context, pp *photoprism.PhotoPrism,
	uids map[string]bool, tmpDir string,
) map[string]photoImage {
	return downloadPhotosWithProgress(ctx, pp, uids, tmpDir, nil)
}

// downloadPhotosWithProgress fetches photos concurrently, reporting progress
// via onProgress after each photo (whether it succeeded or failed) so the
// progress bar reaches 100% even if some photos fail. onProgress may be nil.
func downloadPhotosWithProgress(
	ctx context.Context, pp *photoprism.PhotoPrism,
	uids map[string]bool, tmpDir string,
	onProgress func(ProgressInfo),
) map[string]photoImage {
	result := make(map[string]photoImage)
	var mu sync.Mutex

	total := len(uids)
	if onProgress != nil {
		onProgress(ProgressInfo{Phase: "downloading_photos", Current: 0, Total: total})
	}

	jobs := make(chan string, total)
	for uid := range uids {
		jobs <- uid
	}
	close(jobs)

	var counter atomic.Int64
	var wg sync.WaitGroup
	worker := func() {
		for uid := range jobs {
			if ctx.Err() != nil {
				return
			}
			downloadOnePhoto(pp, uid, tmpDir, result, &mu)
			if onProgress != nil {
				onProgress(ProgressInfo{
					Phase:    "downloading_photos",
					Current:  int(counter.Add(1)),
					Total:    total,
					PhotoUID: uid,
				})
			}
		}
	}
	for range downloadConcurrency {
		wg.Go(worker)
	}
	wg.Wait()
	return result
}

// downloadOnePhoto runs a single photo fetch under the shared result lock.
// Failures are logged and counted as completed so the progress bar advances.
func downloadOnePhoto(
	pp *photoprism.PhotoPrism, uid, tmpDir string,
	result map[string]photoImage, mu *sync.Mutex,
) {
	img, err := downloadPhoto(pp, uid, tmpDir)
	if err != nil {
		log.Printf("WARNING: failed to download photo %s: %v", uid, err)
		return
	}
	mu.Lock()
	result[uid] = *img
	mu.Unlock()
}

// downloadPhoto fetches a single photo thumbnail and returns its path and dimensions.
func downloadPhoto(pp *photoprism.PhotoPrism, uid string, tmpDir string) (*photoImage, error) {
	photos, err := pp.GetPhotosWithQuery(1, 0, "uid:"+uid)
	if err != nil || len(photos) == 0 {
		return nil, fmt.Errorf("photo not found: %s", uid)
	}
	hash := photos[0].Hash
	if hash == "" {
		return nil, fmt.Errorf("photo has no hash: %s", uid)
	}

	data, _, err := pp.GetPhotoThumbnail(hash, "fit_3840")
	if err != nil {
		return nil, fmt.Errorf("failed to download thumbnail: %w", err)
	}

	path := filepath.Join(tmpDir, uid+".jpg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write photo: %w", err)
	}

	// Decode dimensions.
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to open photo: %w", err)
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image config: %w", err)
	}

	sanitizedUID := strings.NewReplacer("\n", "", "\r", "").Replace(uid)
	log.Printf("PDF export: photo %s dimensions %dx%d", sanitizedUID, cfg.Width, cfg.Height)

	return &photoImage{
		path:   path,
		width:  cfg.Width,
		height: cfg.Height,
	}, nil
}

// buildTextSlotNew creates a TemplateSlot for text content in the new 3-zone layout.
func buildTextSlotNew(slot SlotRect, ps database.PageSlot, contentLeftX, canvasTopY float64) TemplateSlot {
	// Convert canvas-relative coords to TikZ page coords.
	clipX := contentLeftX + slot.X
	clipY := canvasTopY - slot.Y - slot.H // TikZ Y from bottom
	return TemplateSlot{
		HasText:     true,
		ClipX:       clipX,
		ClipY:       clipY,
		ClipW:       slot.W,
		ClipH:       slot.H,
		TextContent: ps.TextContent,
		TextType:    DetectTextType(ps.TextContent),
	}
}

// findCaptionsSlotIndex returns the index (into slots) of the slot marked as
// the captions slot, or -1 if the page has none. At most one captions slot is
// allowed per page (enforced at the database layer); if multiple are set for
// any reason, the lowest index wins so behaviour is deterministic.
func findCaptionsSlotIndex(p database.BookPage, slots []SlotRect) int {
	for i := range slots {
		if getPageSlot(p, i).IsCaptions() {
			return i
		}
	}
	return -1
}

// routeCaptionsSlot injects the page's footer captions into the slot marked
// as the captions slot (if any) and clears the returned footer list so the
// bottom strip is suppressed. When the page has no captions slot, both
// arguments are returned unchanged.
func routeCaptionsSlot(
	p database.BookPage, slots []SlotRect, tmplSlots []TemplateSlot,
	footerCaptions []FooterCaption, contentLeftX, canvasTopY float64,
	chapterColor string,
) ([]TemplateSlot, []FooterCaption) {
	idx := findCaptionsSlotIndex(p, slots)
	if idx < 0 {
		return tmplSlots, footerCaptions
	}
	tmplSlots[idx] = buildCaptionsSlotTemplate(
		slots[idx], contentLeftX, canvasTopY, footerCaptions, chapterColor,
	)
	return tmplSlots, nil
}

// buildCaptionsSlotTemplate creates a TemplateSlot that renders the page's
// photo captions stacked vertically inside the slot. The page-level bottom
// captions strip is suppressed when this slot is present.
func buildCaptionsSlotTemplate(
	slot SlotRect, contentLeftX, canvasTopY float64,
	caps []FooterCaption, chapterColor string,
) TemplateSlot {
	return TemplateSlot{
		HasCaptionsList: true,
		ClipX:           contentLeftX + slot.X,
		ClipY:           canvasTopY - slot.Y - slot.H,
		ClipW:           slot.W,
		ClipH:           slot.H,
		CaptionsList:    caps,
		ChapterColor:    chapterColor,
	}
}

// buildPhotoSlotNew creates a TemplateSlot for a photo with object-cover behavior.
// cropX/cropY (0.0-1.0) control the focal point; 0.5 = centered.
// cropScale (0.1-1.0) controls zoom: 1.0 = fill slot, <1.0 = zoom in.
//
//nolint:funlen // Complex layout logic.
func buildPhotoSlotNew(
	slot SlotRect, img photoImage, contentLeftX, canvasTopY float64,
	isArchival bool, archivalInset float64,
	cropX, cropY, cropScale float64,
) TemplateSlot {
	// Border rect (full slot in page coords).
	borderX := contentLeftX + slot.X
	borderY := canvasTopY - slot.Y - slot.H

	// Clip area: inset for archival, same as border for modern.
	clipX := borderX
	clipY := borderY
	clipW := slot.W
	clipH := slot.H
	inset := 0.0

	if isArchival {
		inset = archivalInset
		clipX = borderX + inset
		clipY = borderY + inset
		clipW = slot.W - 2*inset
		clipH = slot.H - 2*inset
	}

	// Object-cover: scale image to fill clip area, centered crop.
	slotAspect := clipW / clipH
	imgAspect := float64(img.width) / float64(img.height)

	var sizeDim string
	var sizeVal, renderW, renderH float64

	if imgAspect > slotAspect {
		sizeDim = sizeDimHeight
		sizeVal = clipH
		renderH = clipH
		renderW = clipH * imgAspect
	} else {
		sizeDim = sizeDimWidth
		sizeVal = clipW
		renderW = clipW
		renderH = clipW / imgAspect
	}

	// Apply crop scale: zoom in by rendering image larger.
	renderW /= cropScale
	renderH /= cropScale
	sizeVal /= cropScale

	overflowX := renderW - clipW
	overflowY := renderH - clipH
	imgX := clipX - overflowX*cropX
	imgY := clipY - overflowY*(1-cropY) // TikZ Y is inverted

	var effectiveDPI float64
	if sizeDim == sizeDimHeight {
		effectiveDPI = float64(img.height) / sizeVal * 25.4
	} else {
		effectiveDPI = float64(img.width) / sizeVal * 25.4
	}
	effectiveDPI = math.Round(effectiveDPI*10) / 10

	return TemplateSlot{
		HasPhoto:     true,
		ClipX:        clipX,
		ClipY:        clipY,
		ClipW:        clipW,
		ClipH:        clipH,
		ImgX:         imgX,
		ImgY:         imgY,
		SizeDim:      sizeDim,
		SizeVal:      sizeVal,
		FilePath:     img.path,
		EffectiveDPI: effectiveDPI,
		IsArchival:   isArchival,
		MatInsetMM:   inset,
		BorderX:      borderX,
		BorderY:      borderY,
		BorderW:      slot.W,
		BorderH:      slot.H,
	}
}

// setEnvVars returns a copy of environ with the given vars set, replacing any
// existing entries with the same key. This avoids duplicate env entries where
// only the first occurrence is used by most libc implementations.
func setEnvVars(environ []string, vars map[string]string) []string {
	result := make([]string, 0, len(environ)+len(vars))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if _, override := vars[key]; !override {
			result = append(result, entry)
		}
	}
	for k, v := range vars {
		result = append(result, k+"="+v)
	}
	return result
}

// SinglePageInput holds context needed to render a single page as PDF.
type SinglePageInput struct {
	Page         database.BookPage
	Book         *database.PhotoBook // book with typography settings (nil = defaults)
	ChapterColor string              // hex without # (e.g. "8B0000"), empty = no color
	Captions     CaptionMap
	PageNumber   int // actual 1-based page number in the full book; values < 1 are clamped to 1
}

// GenerateSinglePagePDF renders a single book page to PDF using the exact same
// layout, template, and compile path as full book export. Callers MUST set
// input.PageNumber to the page's 1-based position in the full book so that
// margins, folio placement, and the printed page number match what the full
// book render would produce for that page.
func GenerateSinglePagePDF(
	ctx context.Context, pp *photoprism.PhotoPrism, input SinglePageInput,
) ([]byte, error) {
	uidSet := collectPhotoUIDs([]database.BookPage{input.Page})

	tmpDir, err := os.MkdirTemp("", "page-pdf-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	photos := downloadPhotos(ctx, pp, uidSet, tmpDir)
	config := DefaultLayoutConfig()
	typo := resolveBookTypography(input.Book)

	pageNum := max(input.PageNumber, 1)

	pb := &pageBuilder{
		config:            config,
		photos:            photos,
		captions:          input.Captions,
		totalContentPages: pageNum,
		contentPageIdx:    pageNum - 1,
		pageNumber:        pageNum - 1, // incremented to pageNum below
		photoSet:          make(map[string]bool),
		headingColorBleed: typo.headingColorBleed,
	}

	pb.contentPageIdx++
	pb.pageNumber++
	tmplPage := pb.buildContentPage(input.Page, input.ChapterColor)

	data := TemplateData{
		Sections: []TemplateSection{{
			Title: "",
			Pages: []TemplatePage{tmplPage},
		}},
		PageW:                  PageW,
		PageH:                  PageH,
		BodyFontDeclaration:    typo.bodyFontDeclaration,
		HeadingFontDeclaration: typo.headingFontDeclaration,
		BodyFontSize:           typo.bodyFontSize,
		BodyLineHeight:         typo.bodyLineHeight,
		H1FontSize:             typo.h1FontSize,
		H1Leading:              typo.h1Leading,
		H2FontSize:             typo.h2FontSize,
		H2Leading:              typo.h2Leading,
		CaptionOpacity:         typo.captionOpacity,
		CaptionFontSize:        typo.captionFontSize,
		CaptionLeading:         typo.captionLeading,
		CaptionBadgeSize:       typo.captionBadgeSize,
	}

	pdfData, err := compileLatex(ctx, data, tmpDir)
	if err != nil {
		return nil, err
	}
	return pdfData, nil
}
