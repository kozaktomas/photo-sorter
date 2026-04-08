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
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

//go:embed templates/book.tex
var templateFS embed.FS

// --- Export Report Types ---

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
// Marker is a formatted string like "1", "1–3", or "1, 3" for merged captions.
type FooterCaption struct {
	Marker  string
	Caption string
}

// TemplateSlot holds pre-computed TikZ coordinates for one photo or text slot.
type TemplateSlot struct {
	HasPhoto bool
	HasText  bool
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
	Slots      []TemplateSlot
	IsLast     bool
	PageNumber int    // continuous page number (1-based)
	IsRecto    bool   // true for odd pages (right-hand, recto)
	Style      string // "modern" or "archival"
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
	SinglePage      bool // true for single-page export (oneside, no blank pages)
	DebugOverlay    bool
	DebugColOffsets []float64 // relative X offsets for column left edges

	// Typography settings (from per-book configuration).
	BodyFontLatex    string  // fontspec font name, e.g. "PT Serif"
	HeadingFontLatex string  // fontspec font name, e.g. "Source Sans 3"
	BodyFontSize     float64 // e.g. 11.0
	BodyLineHeight   float64 // e.g. 15.0
	H1FontSize       float64 // e.g. 18.0
	H1Leading        float64 // e.g. 22.0
	H2FontSize       float64 // e.g. 13.0
	H2Leading        float64 // e.g. 16.0
	CaptionOpacity   int     // 0-100 for LaTeX black!N notation
	CaptionFontSize  float64 // e.g. 9.0
	CaptionLeading   float64 // e.g. 11.0
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
	return GeneratePDFWithOptions(ctx, pp, br, bookID, false)
}

// GeneratePDFWithOptions renders a photo book to PDF with optional debug overlay.
//
//nolint:cyclop // Orchestration function that fetches multiple resources.
func GeneratePDFWithOptions(
	ctx context.Context, pp *photoprism.PhotoPrism,
	br database.BookReader, bookID string, debug bool,
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

	photos := downloadPhotos(ctx, pp, uidSet, tmpDir)
	groups := groupPagesBySection(pages, sections, chapters)
	config := DefaultLayoutConfig()
	data, report := buildTemplateData(groups, photos, captions, config, book)

	if debug {
		applyDebugOverlay(&data, config)
	}

	// Layout validation.
	validationWarnings := ValidatePages(data.Sections, config)
	for _, vw := range validationWarnings {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Layout: page %d slot %d: %s", vw.PageNumber, vw.SlotIndex, vw.Message))
	}

	addDPIWarnings(report)

	pdfData, err := compileLatex(ctx, data, tmpDir)
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
			Sections:         tmplSections,
			PageW:            PageW,
			PageH:            PageH,
			BodyFontLatex:    typo.bodyFontLatex,
			HeadingFontLatex: typo.headingFontLatex,
			BodyFontSize:     typo.bodyFontSize,
			BodyLineHeight:   typo.bodyLineHeight,
			H1FontSize:       typo.h1FontSize,
			H1Leading:        typo.h1Leading,
			H2FontSize:       typo.h2FontSize,
			H2Leading:        typo.h2Leading,
			CaptionOpacity:   typo.captionOpacity,
			CaptionFontSize:  typo.captionFontSize,
			CaptionLeading:   typo.captionLeading,
		}, &ExportReport{
			BookTitle:  bookTitle,
			PageCount:  pb.pageNumber,
			PhotoCount: len(pb.photoSet),
			Pages:      pb.reportPages,
		}
}

// resolvedTypography holds resolved font/size values with defaults applied.
type resolvedTypography struct {
	bodyFontLatex     string
	headingFontLatex  string
	bodyFontSize      float64
	bodyLineHeight    float64
	h1FontSize        float64
	h1Leading         float64
	h2FontSize        float64
	h2Leading         float64
	captionOpacity    int
	captionFontSize   float64
	captionLeading    float64
	headingColorBleed float64
}

// resolveBookTypography resolves typography settings from a PhotoBook with fallbacks.
func resolveBookTypography(book *database.PhotoBook) resolvedTypography {
	rt := resolvedTypography{
		bodyFontLatex:     "PT Serif",
		headingFontLatex:  "Source Sans 3",
		bodyFontSize:      DefaultBodyFontSize,
		bodyLineHeight:    DefaultBodyLineHeight,
		h1FontSize:        DefaultH1FontSize,
		h2FontSize:        DefaultH2FontSize,
		captionOpacity:    int(DefaultCaptionOpacity * 100),
		captionFontSize:   DefaultCaptionFontSize,
		headingColorBleed: DefaultHeadingColorBleed,
	}

	if book != nil {
		applyBookFonts(&rt, book)
		applyBookSizes(&rt, book)
	}

	rt.h1Leading = math.Ceil(rt.h1FontSize * 1.22)
	rt.h2Leading = math.Ceil(rt.h2FontSize * 1.23)
	rt.captionLeading = math.Ceil(rt.captionFontSize * 1.22)
	return rt
}

// applyBookFonts overrides font names from book settings when available.
func applyBookFonts(rt *resolvedTypography, book *database.PhotoBook) {
	if f, ok := GetFont(book.BodyFont); ok && f.LatexName != "" {
		rt.bodyFontLatex = f.LatexName
	}
	if f, ok := GetFont(book.HeadingFont); ok && f.LatexName != "" {
		rt.headingFontLatex = f.LatexName
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
	if book.HeadingColorBleed > 0 {
		rt.headingColorBleed = book.HeadingColorBleed
	}
}

// isPageEmpty returns true if a page has no content (no photos, no text in any slot).
func isPageEmpty(p database.BookPage) bool {
	if len(p.Slots) == 0 {
		return true
	}
	for _, s := range p.Slots {
		if s.PhotoUID != "" || s.TextContent != "" {
			return false
		}
	}
	return true
}

// buildSection builds a TemplateSection and accumulates report data.
func (pb *pageBuilder) buildSection(g sectionGroup) TemplateSection {
	tmplPages := make([]TemplatePage, 0, len(g.pages))
	for _, p := range g.pages {
		if isPageEmpty(p) {
			pb.totalContentPages-- // adjust total so IsLast works correctly
			continue
		}
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

	pb.reportPages = append(pb.reportPages, ReportPage{
		PageNumber: pb.pageNumber,
		Format:     p.Format,
		Title:      p.Description,
		Photos:     reportPhotos,
	})

	// Caption block position.
	var captionBlockX, captionBlockY, captionBlockW float64
	hasCaptions := len(footerCaptions) > 0
	if hasCaptions {
		captionBlockX = contentLeftX
		captionBlockY = footerRuleY - 4.0 // 1 baseline unit below rule
		captionBlockW = contentW
	}

	// Expand clip bounds by heading bleed so colored heading boxes
	// extend into the margins. Photos stay constrained by slot-level clips.
	bleed := pb.headingColorBleed
	clipLeftX := max(0, contentLeftX-bleed)
	clipRightX := min(PageW, contentRightX+bleed)

	return TemplatePage{
		Slots:         tmplSlots,
		IsLast:        isLast,
		PageNumber:    pb.pageNumber,
		IsRecto:       isRecto,
		Style:         style,
		ContentLeftX:  contentLeftX,
		ContentRightX: contentRightX,
		ContentW:      contentW,
		ClipLeftX:     clipLeftX,
		ClipRightX:    clipRightX,
		HeaderY:       headerY,
		CanvasTopY:    canvasTopY,
		CanvasBottomY: canvasBottomY,
		FooterRuleY:   footerRuleY,
		FolioX:        folioX,
		FolioY:        folioY,
		FolioAnchor:   folioAnchor,
		Captions:      footerCaptions,
		CaptionBlockX: captionBlockX,
		CaptionBlockY: captionBlockY,
		CaptionBlockW: captionBlockW,
		HasCaptions:   hasCaptions,
	}
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
func buildFooterCaptions(ct captionTracking) []FooterCaption {
	footerCaptions := make([]FooterCaption, 0, len(ct.captions))
	for _, sc := range ct.captions {
		marker := ct.markerMap[sc.slotIdx]
		markerStr := ""
		if marker > 0 {
			markerStr = strconv.Itoa(marker)
		}
		footerCaptions = append(footerCaptions, FooterCaption{
			Marker:  markerStr,
			Caption: sc.caption,
		})
	}
	return mergeFooterCaptions(footerCaptions)
}

// mergeFooterCaptions groups footer captions by identical text and merges their
// marker numbers into ranges (e.g. "1–3") or comma-separated lists (e.g. "1, 3").
// Order is preserved by first occurrence.
func mergeFooterCaptions(caps []FooterCaption) []FooterCaption {
	if len(caps) <= 1 {
		return caps
	}

	type group struct {
		caption string
		markers []int
	}

	var groups []group
	seen := make(map[string]int) // caption text -> index in groups

	for _, c := range caps {
		num, _ := strconv.Atoi(c.Marker)
		idx, ok := seen[c.Caption]
		if ok {
			groups[idx].markers = append(groups[idx].markers, num)
		} else {
			seen[c.Caption] = len(groups)
			groups = append(groups, group{caption: c.Caption, markers: []int{num}})
		}
	}

	result := make([]FooterCaption, 0, len(groups))
	for _, g := range groups {
		sort.Ints(g.markers)
		result = append(result, FooterCaption{
			Marker:  formatSlotNumbers(g.markers),
			Caption: g.caption,
		})
	}
	return result
}

// formatSlotNumbers formats a sorted slice of marker numbers into a compact
// string using en-dashes for consecutive ranges and commas otherwise.
// Examples: [1] -> "1", [1,2,3] -> "1–3", [1,3] -> "1, 3", [1,2,3,5] -> "1–3, 5".
func formatSlotNumbers(nums []int) string {
	if len(nums) == 0 {
		return ""
	}
	if len(nums) == 1 {
		return strconv.Itoa(nums[0])
	}

	var parts []string
	rangeStart := nums[0]
	rangeEnd := nums[0]

	for i := 1; i < len(nums); i++ {
		if nums[i] == rangeEnd+1 {
			rangeEnd = nums[i]
		} else {
			parts = append(parts, formatRange(rangeStart, rangeEnd))
			rangeStart = nums[i]
			rangeEnd = nums[i]
		}
	}
	parts = append(parts, formatRange(rangeStart, rangeEnd))
	return strings.Join(parts, ", ")
}

// formatRange formats a single range: "1" for a single number, "1–3" for a range.
func formatRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "\u2013" + strconv.Itoa(end)
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

	return tmplSlots, reportPhotos, buildFooterCaptions(ct)
}

// textPadMM is the inner padding (mm) added to text slots adjacent to photos.
const textPadMM = 4.0

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

// applyTextSlotPadding sets heading bleed and inner padding on text slots.
// headingBleed extends the colored heading box equally on both sides.
// Non-edge sides also get text padding to offset body text from adjacent photos.
func applyTextSlotPadding(slots []TemplateSlot, format string, headingBleed float64) {
	for i := range slots {
		if !slots[i].HasText {
			continue
		}
		// Heading bleed always extends both sides equally.
		slots[i].BleedLeftMM = headingBleed
		slots[i].BleedRightMM = headingBleed

		// Non-edge sides get body text padding to keep text away from adjacent photos.
		if !isSlotOnLeftEdge(format, i) {
			slots[i].TextPadLeft = textPadMM
		}
		if !isSlotOnRightEdge(format, i) {
			slots[i].TextPadRight = textPadMM
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

// compileLatex writes the template and runs lualatex, returning the PDF bytes.
func compileLatex(ctx context.Context, data TemplateData, tmpDir string) ([]byte, error) {
	typoConfig := TypographyConfig{
		H1Size:    data.H1FontSize,
		H1Leading: data.H1Leading,
		H2Size:    data.H2FontSize,
		H2Leading: data.H2Leading,
	}
	funcMap := template.FuncMap{
		"latexEscape":     latexEscape,
		"markdownToLatex": MarkdownToLatex,
		"markdownToLatexColor": func(md, color string, bleedL, bleedR float64) string {
			return MarkdownToLatexWithTypography(md, color, bleedL, bleedR, typoConfig)
		},
		"contrastTextColor": func(hexColor string) string {
			if hexColor == "" {
				return "white"
			}
			return contrastTextColorLatex(hexColor)
		},
		"addFloat":      func(a, b float64) float64 { return a + b },
		"subtractFloat": func(a, b float64) float64 { return a - b },
		"mulFloat":      func(a, b float64) float64 { return a * b },
		"divFloat":      func(a, b float64) float64 { return a / b },
	}
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
	// All TEXMF* and HOME vars are set to tmpDir so luaotfload builds a fresh
	// font database there. The pre-built Docker cache at /var/cache/luatex-cache
	// cannot be used because luaotfload's cache detection fails when lualatex is
	// invoked via Go's exec.Command (works from shell but not from Go — likely a
	// difference in how the fontloader-basics-gen cache writability check behaves
	// with Go's fork+exec). Font database generation adds ~2s per export.
	latexEnv := setEnvVars(os.Environ(), map[string]string{
		"TEXMFCACHE": tmpDir,
		"TEXMFVAR":   tmpDir,
		"HOME":       tmpDir,
	})
	for pass := range 2 {
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
	result := make(map[string]photoImage)
	var mu sync.Mutex

	jobs := make(chan string, len(uids))
	for uid := range uids {
		jobs <- uid
	}
	close(jobs)

	var wg sync.WaitGroup
	for range downloadConcurrency {
		wg.Go(func() {
			for uid := range jobs {
				if ctx.Err() != nil {
					return
				}
				img, err := downloadPhoto(pp, uid, tmpDir)
				if err != nil {
					log.Printf("WARNING: failed to download photo %s: %v", uid, err)
					continue
				}
				mu.Lock()
				result[uid] = *img
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return result
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
	PageNumber   int // actual page number in book (1-based); 0 = default to 1
}

// GenerateSinglePagePDF renders a single book page to PDF using the same layout as full book export.
// When PageNumber is set, the page is rendered with the correct page number, margins, and side
// (recto/verso) matching its position in the full book.
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
		SinglePage:       true,
		PageW:            PageW,
		PageH:            PageH,
		BodyFontLatex:    typo.bodyFontLatex,
		HeadingFontLatex: typo.headingFontLatex,
		BodyFontSize:     typo.bodyFontSize,
		BodyLineHeight:   typo.bodyLineHeight,
		H1FontSize:       typo.h1FontSize,
		H1Leading:        typo.h1Leading,
		H2FontSize:       typo.h2FontSize,
		H2Leading:        typo.h2Leading,
		CaptionOpacity:   typo.captionOpacity,
		CaptionFontSize:  typo.captionFontSize,
		CaptionLeading:   typo.captionLeading,
	}

	pdfData, err := compileLatex(ctx, data, tmpDir)
	if err != nil {
		return nil, err
	}
	return pdfData, nil
}
