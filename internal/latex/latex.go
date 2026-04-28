package latex

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
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

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

// PhotoQuality selects the photo resolution tier used when building the PDF.
// The zero value ("" or QualityDefault) is equivalent to QualityMedium.
type PhotoQuality string

// Quality tiers for PDF photo export. Medium is the default and matches the
// historical behaviour (fit_3840 thumbnails). Low is for quick previews.
// Original downloads the primary file and resizes it to at most
// OriginalMaxLongestSidePx on the longest side before embedding.
const (
	QualityDefault  PhotoQuality = ""
	QualityLow      PhotoQuality = "low"
	QualityMedium   PhotoQuality = "medium"
	QualityOriginal PhotoQuality = "original"

	// OriginalMaxLongestSidePx is the cap applied to the longest side of an
	// image when PhotoQuality is QualityOriginal. Photos whose longest side
	// exceeds this value are downscaled before being embedded in the PDF so
	// the resulting document stays within a reasonable size. 8000 px is
	// roughly 300 DPI at A3 and well above what any realistic book layout
	// needs.
	OriginalMaxLongestSidePx = 8000

	// originalJPEGQuality is the encoder quality used when re-saving a
	// decoded original photo. 92 balances print-quality detail with file
	// size on the final PDF.
	originalJPEGQuality = 92

	// thumbnailLow / thumbnailMedium / thumbnailOriginalFallback are the
	// PhotoPrism thumbnail sizes used for each quality tier.
	thumbnailLow              = "fit_720"
	thumbnailMedium           = "fit_3840"
	thumbnailOriginalFallback = "fit_7680"
)

// normalizeQuality returns the concrete PhotoQuality for q, treating the zero
// value as QualityMedium.
func normalizeQuality(q PhotoQuality) PhotoQuality {
	switch q {
	case QualityLow, QualityOriginal:
		return q
	default:
		return QualityMedium
	}
}

// ValidatePhotoQuality returns a normalized PhotoQuality for s, or an error if
// s is not a supported value. Empty string is treated as the default
// (QualityMedium). This is exported for HTTP handlers that validate a query
// parameter coming from the user.
func ValidatePhotoQuality(s string) (PhotoQuality, error) {
	switch PhotoQuality(s) {
	case QualityDefault, QualityMedium:
		return QualityMedium, nil
	case QualityLow:
		return QualityLow, nil
	case QualityOriginal:
		return QualityOriginal, nil
	default:
		return QualityMedium, fmt.Errorf("invalid photo_quality %q (want low, medium, or original)", s)
	}
}

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
//
// PhotoQuality selects the photo resolution tier: QualityLow (fit_720) for
// quick previews, QualityMedium (fit_3840) for the default behaviour, and
// QualityOriginal (primary file, resized to at most OriginalMaxLongestSidePx)
// for print. The zero value is treated as QualityMedium.
type ExportOptions struct {
	Debug        bool
	OnProgress   func(ProgressInfo)
	PhotoQuality PhotoQuality
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

// TOCSection is one row in the book's table of contents: a section title
// with the printed page range it occupies (StartPage..EndPage, inclusive,
// 1-based). StartPage == EndPage for a single-page section.
type TOCSection struct {
	Title     string
	StartPage int
	EndPage   int
}

// TOCChapter is one chapter block in the book's table of contents, with its
// ordered list of section entries. Chapters appear in book order; sections
// inside a chapter appear in the order they are first encountered in the
// book. A chapter with an empty Title represents sections that have no
// chapter assignment (they are still rendered under a blank chapter line).
//
// StartsRightColumn is set to true by balanceTOCColumns on the chapter that
// should open the right column of the two-column TOC slot, so the LaTeX
// template can emit a \columnbreak before that chapter and no chapter is
// ever split across columns.
type TOCChapter struct {
	Title             string
	Sections          []TOCSection
	StartsRightColumn bool
}

// TemplateSlot holds pre-computed TikZ coordinates for one photo, text,
// captions, or contents slot.
type TemplateSlot struct {
	HasPhoto        bool
	HasText         bool
	HasCaptionsList bool
	HasContents     bool
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
	// ContentsHeader is the headline rendered at the top of a contents slot
	// (e.g. "Obsah"). Empty = no headline.
	ContentsHeader string
	// ContentsEntries holds the book's table of contents (chapters with
	// sections and page ranges) when this slot is the contents slot. The
	// template renders them as a two-column list with dotted leaders. Set by
	// injectContentsSlots after all pages are built so section page ranges
	// are known.
	ContentsEntries []TOCChapter
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
	CaptionMarker         int
	CaptionMarkerX        float64 // bottom-left X of marker rect
	CaptionMarkerY        float64 // bottom-left Y of marker rect
	CaptionMarkerCenterX  float64 // center X for number node
	CaptionMarkerCenterY  float64 // center Y for number node
	CaptionMarkerSize     float64 // square dimension (mm) — same as caption_badge_size
	CaptionMarkerFontSize float64 // pt — derived as size_mm × 1.5
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
	sectionID          string
	title              string
	chapterID          string // empty = section has no chapter
	chapterTitle       string
	chapterColor       string // hex color without # (e.g. "8B0000"), empty = no color
	chapterHideFromTOC bool   // true = skip this section's chapter when rendering the TOC
	pages              []database.BookPage
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
	captionBadgeSize  float64
	bodyTextPadMM     float64
	// sectionPageRanges maps sectionID -> [startPage, endPage] (1-based,
	// inclusive) for the printed page range of each section, populated as
	// buildSection runs. Used by buildTOCData to compute table-of-contents
	// entries for the contents slot.
	sectionPageRanges map[string][2]int
}

const (
	downloadConcurrency = 5
	// originalDownloadConcurrency caps parallel workers when fetching photos at
	// QualityOriginal. Originals can require a full image.Image decode (and a
	// second RGBA buffer for the resize step) per worker, so keeping this low
	// bounds the peak resident memory of the export.
	originalDownloadConcurrency = 2
	lowResDPIThreshold          = 200.0
	sizeDimHeight               = "height"
	sizeDimWidth                = "width"
	// clipSafetyMM is the minimum horizontal expansion of the canvas clip
	// rectangle beyond the content area. It guarantees that justified body
	// text on a page-edge text slot has room for typographic overflow
	// (comma, hyphenation hyphen, italic descender) even when the user
	// configures heading_color_bleed = 0.
	clipSafetyMM = 1.0
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

	photos := downloadPhotosWithProgress(ctx, pp, uidSet, tmpDir, opts.OnProgress, normalizeQuality(opts.PhotoQuality))
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

	chapterTitles := make(map[string]string, len(chapters))
	chapterColors := make(map[string]string, len(chapters))
	chapterHideFromTOC := make(map[string]bool, len(chapters))
	for _, c := range chapters {
		chapterTitles[c.ID] = c.Title
		if c.Color != "" {
			chapterColors[c.ID] = strings.TrimPrefix(c.Color, "#")
		}
		chapterHideFromTOC[c.ID] = c.HideFromTOC
	}

	var groups []sectionGroup
	lastSectionID := ""
	for _, p := range pages {
		if p.SectionID != lastSectionID {
			chapterID := sectionChapters[p.SectionID]
			groups = append(groups, sectionGroup{
				sectionID:          p.SectionID,
				title:              sectionTitles[p.SectionID],
				chapterID:          chapterID,
				chapterTitle:       chapterTitles[chapterID],
				chapterColor:       chapterColors[chapterID],
				chapterHideFromTOC: chapterHideFromTOC[chapterID],
				pages:              []database.BookPage{p},
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
		captionBadgeSize:  typo.captionBadgeSize,
		bodyTextPadMM:     typo.bodyTextPadMM,
		sectionPageRanges: make(map[string][2]int),
	}
	for _, g := range groups {
		pb.totalContentPages += len(g.pages)
	}

	tmplSections := make([]TemplateSection, 0, len(groups))
	for _, g := range groups {
		tmplSections = append(tmplSections, pb.buildSection(g))
	}

	// Second pass: now that every section knows its page range, fill in the
	// book's table of contents for any slot flagged as a contents slot.
	injectContentsSlots(tmplSections, buildTOCData(groups, pb.sectionPageRanges))

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
	bodyTextPadMM          float64
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
		bodyTextPadMM:          DefaultBodyTextPadMM,
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
	// body_text_pad_mm is NOT NULL in the DB; 0 is an explicit user choice
	// (no inner padding — body text reaches the slot edge), so always assign.
	rt.bodyTextPadMM = book.BodyTextPadMM
}

// buildSection builds a TemplateSection and accumulates report data. Every
// page from the section group is rendered, including pages with no photos
// and no text — they are preserved as blank pages so they keep a folio
// number and shift the pagination of pages that follow them.
//
// As pages are emitted, sectionPageRanges[sectionID] is updated with the
// first and last printed page number for this section so the table of
// contents can report accurate page ranges.
func (pb *pageBuilder) buildSection(g sectionGroup) TemplateSection {
	tmplPages := make([]TemplatePage, 0, len(g.pages))
	startPage := pb.pageNumber + 1
	for _, p := range g.pages {
		pb.contentPageIdx++
		pb.pageNumber++
		tmplPages = append(tmplPages, pb.buildContentPage(p, g.chapterColor))
	}
	if len(g.pages) > 0 && g.sectionID != "" {
		pb.sectionPageRanges[g.sectionID] = [2]int{startPage, pb.pageNumber}
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

// buildAndRouteSlots builds slot templates for a non-fullbleed page and
// routes the captions slot (if any) before returning.
func (pb *pageBuilder) buildAndRouteSlots(
	p database.BookPage, contentLeftX, canvasTopY float64,
	style string, isRecto bool, chapterColor string,
) ([]TemplateSlot, []ReportPhoto, []FooterCaption) {
	slots := FormatSlotsGridWithSplit(p.Format, pb.config, p.SplitPosition)
	tmplSlots, reportPhotos, footerCaptions := pb.buildSlots(
		p, slots, contentLeftX, canvasTopY, style, isRecto, chapterColor,
	)
	tmplSlots, footerCaptions = routeCaptionsSlot(
		p, slots, tmplSlots, footerCaptions, contentLeftX, canvasTopY, chapterColor,
	)
	return tmplSlots, reportPhotos, footerCaptions
}

// buildContentPage builds a single TemplatePage with slots and accumulates report data.
func (pb *pageBuilder) buildContentPage(p database.BookPage, chapterColor string) TemplatePage {
	if p.Format == FormatFullbleed {
		return pb.buildFullBleedPage(p, chapterColor)
	}

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

	tmplSlots, reportPhotos, footerCaptions := pb.buildAndRouteSlots(
		p, contentLeftX, canvasTopY, style, isRecto, chapterColor,
	)

	pb.reportPages = append(pb.reportPages, ReportPage{
		PageNumber: pb.pageNumber,
		Format:     p.Format,
		Title:      p.Description,
		Photos:     reportPhotos,
	})

	captionBlockX, captionBlockY, captionBlockW, hasCaptions :=
		captionBlockPosition(footerCaptions, contentLeftX, contentW, footerRuleY)

	// Expand clip bounds by heading bleed so colored heading boxes extend
	// into the margins. Photos stay constrained by slot-level clips.
	// A small typographic safety margin is always applied (independent of
	// the heading bleed) so justified body text on a page-edge text slot
	// doesn't lose its last glyph (comma, hyphenation hyphen, italic
	// descender) when the user sets heading_color_bleed = 0.
	bleed := max(pb.headingColorBleed, clipSafetyMM)
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

// buildFullBleedPage builds a 1_fullbleed page: a single photo covering the
// entire bleed area (303×216mm = A4 + 3mm bleed on every side). Folio and
// footer captions are suppressed automatically; only the photo renders.
//
// This path is fully separate from buildContentPage's grid+canvas pipeline:
// it places the photo via buildPhotoSlotNew with substituted contentLeftX
// and canvasTopY so the resulting border/clip rectangle covers (-3,-3) to
// (300,213) in TikZ page coordinates. The TemplatePage's clip bounds are
// expanded to the same area so the canvas-level clip in the template does
// not crop the photo back to the safe area.
func (pb *pageBuilder) buildFullBleedPage(p database.BookPage, chapterColor string) TemplatePage {
	isLast := pb.contentPageIdx == pb.totalContentPages
	isRecto := pb.pageNumber%2 == 1

	// Compute zone fields for struct consistency. None of them affect the
	// rendered output for fullbleed (template branches on Slots / HasCaptions
	// / HidePageNumber, all of which we override below) but we fill them so
	// the TemplatePage struct stays consistent if some downstream code reads
	// them by mistake. canvasTopY / canvasBottomY are intentionally
	// discarded — they are replaced by the bleed-extended values below.
	contentLeftX, contentRightX, headerY, _, _,
		footerRuleY, folioX, folioY, folioAnchor := pb.computeZones(isRecto)

	tmplSlots, reportPhotos := pb.buildFullBleedPhotoSlot(p, chapterColor)

	pb.reportPages = append(pb.reportPages, ReportPage{
		PageNumber: pb.pageNumber,
		Format:     p.Format,
		Title:      p.Description,
		Photos:     reportPhotos,
	})

	return TemplatePage{
		Slots:          tmplSlots,
		IsLast:         isLast,
		PageNumber:     pb.pageNumber,
		IsRecto:        isRecto,
		Style:          "modern",
		HidePageNumber: true,
		ContentLeftX:   contentLeftX,
		ContentRightX:  contentRightX,
		ContentW:       pb.config.ContentWidth(),
		ClipLeftX:      -BleedMM - fullBleedRasterEpsilonMM,
		ClipRightX:     PageW + BleedMM + fullBleedRasterEpsilonMM,
		HeaderY:        headerY,
		CanvasTopY:     PageH + BleedMM + fullBleedRasterEpsilonMM,
		CanvasBottomY:  -BleedMM - fullBleedRasterEpsilonMM,
		FooterRuleY:    footerRuleY,
		FolioX:         folioX,
		FolioY:         folioY,
		FolioAnchor:    folioAnchor,
		Captions:       nil,
		HasCaptions:    false,
	}
}

// buildFullBleedPhotoSlot builds the single full-bleed photo slot for a
// 1_fullbleed page. Returns nil slices if the page has no photo assigned
// or the photo isn't in the fetched set.
func (pb *pageBuilder) buildFullBleedPhotoSlot(
	p database.BookPage, chapterColor string,
) ([]TemplateSlot, []ReportPhoto) {
	ps := getPageSlot(p, 0)
	if ps.PhotoUID == "" {
		return nil, nil
	}
	img, ok := pb.photos[ps.PhotoUID]
	if !ok {
		return nil, nil
	}
	cropScale := ps.CropScale
	if cropScale <= 0 {
		cropScale = 1.0
	}
	// eps expands the slot past the media box so rasterizers don't leave
	// a sub-mm white row at the bottom (see formats.go).
	eps := fullBleedRasterEpsilonMM
	fullSlot := SlotRect{
		X: 0, Y: 0,
		W: PageW + 2*BleedMM + 2*eps,
		H: PageH + 2*BleedMM + 2*eps,
	}
	ts := buildPhotoSlotNew(
		fullSlot, img,
		-BleedMM-eps, PageH+BleedMM+eps,
		false, 0,
		ps.CropX, ps.CropY, cropScale,
	)
	ts.ChapterColor = chapterColor
	pb.photoSet[ps.PhotoUID] = true
	return []TemplateSlot{ts}, []ReportPhoto{{
		PhotoUID:     ps.PhotoUID,
		SlotIndex:    0,
		EffectiveDPI: ts.EffectiveDPI,
		LowRes:       ts.EffectiveDPI > 0 && ts.EffectiveDPI < lowResDPIThreshold,
	}}
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
// badgeSize (mm) drives both the rectangle dimensions and the inner font size
// (font_pt = size_mm × 1.5), matching the footer caption badge so the two
// always render identically.
func placeCaptionMarker(ts *TemplateSlot, markerNum int, badgeSize float64, isRecto bool) {
	ts.CaptionMarker = markerNum
	if markerNum <= 0 {
		return
	}
	markerSize := badgeSize
	markerInset := markerSize / 2.0
	if isRecto {
		ts.CaptionMarkerX = ts.ClipX + ts.ClipW - markerInset - markerSize
	} else {
		ts.CaptionMarkerX = ts.ClipX + markerInset
	}
	ts.CaptionMarkerY = ts.ClipY + ts.ClipH - markerInset - markerSize
	ts.CaptionMarkerCenterX = ts.CaptionMarkerX + markerSize/2
	ts.CaptionMarkerCenterY = ts.CaptionMarkerY + markerSize/2
	ts.CaptionMarkerSize = markerSize
	ts.CaptionMarkerFontSize = markerSize * 1.5
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

// buildNonPhotoSlot routes a PageSlot that is not a photo slot (text,
// contents) to its TemplateSlot builder and sets ChapterColor. Returns
// (nil, true) for a recognized-but-empty placeholder like an unflagged empty
// slot reached via this helper. Returns (_, false) when the caller must fall
// through to photo handling.
func buildNonPhotoSlot(
	slot SlotRect, ps database.PageSlot,
	contentLeftX, canvasTopY float64, chapterColor string,
) (*TemplateSlot, bool) {
	switch {
	case ps.IsTextSlot():
		ts := buildTextSlotNew(slot, ps, contentLeftX, canvasTopY)
		ts.ChapterColor = chapterColor
		return &ts, true
	case ps.IsContents():
		ts := buildContentsSlotStub(slot, contentLeftX, canvasTopY)
		ts.ChapterColor = chapterColor
		return &ts, true
	}
	return nil, false
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

		if ts, handled := buildNonPhotoSlot(slot, ps, contentLeftX, canvasTopY, chapterColor); handled {
			if ts != nil {
				tmplSlots[i] = *ts
			}
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
		placeCaptionMarker(&ts, ct.markerMap[i], pb.captionBadgeSize, isRecto)
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

	applyTextSlotPadding(tmplSlots, p.Format, pb.headingColorBleed, pb.bodyTextPadMM)

	return tmplSlots, reportPhotos, buildFooterCaptions(ct, chapterColor)
}

// isSlotOnLeftEdge returns true if the slot touches the left page margin.
func isSlotOnLeftEdge(format string, slotIndex int) bool {
	switch format {
	case FormatFullscreen:
		return true
	case FormatFullbleed:
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
	case FormatFullbleed:
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

// neighborKey identifies a (format, slotIndex) pair for the neighbor maps.
type neighborKey struct {
	format string
	slot   int
}

// leftNeighborMap holds, for each text-capable format/slot, the indices of
// slots horizontally adjacent on the left. Missing keys mean "no left
// neighbor" (i.e. the slot sits on the left page edge).
var leftNeighborMap = map[neighborKey][]int{
	{Format2Portrait, 1}:  {0},
	{Format4Landscape, 1}: {0},
	{Format4Landscape, 3}: {2},
	{Format1P2L, 1}:       {0},
	{Format1P2L, 2}:       {0},
	{Format2L1P, 2}:       {0, 1},
}

// rightNeighborMap is the symmetric counterpart of leftNeighborMap.
var rightNeighborMap = map[neighborKey][]int{
	{Format2Portrait, 0}:  {1},
	{Format4Landscape, 0}: {1},
	{Format4Landscape, 2}: {3},
	{Format1P2L, 0}:       {1, 2},
	{Format2L1P, 0}:       {2},
	{Format2L1P, 1}:       {2},
}

// leftNeighbors returns indices of slots horizontally adjacent on the left
// of slotIndex within the same row(s). Empty for slots on the left page edge.
func leftNeighbors(format string, slotIndex int) []int {
	return leftNeighborMap[neighborKey{format, slotIndex}]
}

// rightNeighbors returns indices of slots horizontally adjacent on the right
// of slotIndex within the same row(s). Empty for slots on the right page edge.
func rightNeighbors(format string, slotIndex int) []int {
	return rightNeighborMap[neighborKey{format, slotIndex}]
}

// anyNeighborIsPhoto returns true if any slot at the given indices is a photo.
func anyNeighborIsPhoto(slots []TemplateSlot, neighbors []int) bool {
	for _, idx := range neighbors {
		if idx < 0 || idx >= len(slots) {
			continue
		}
		if slots[idx].HasPhoto {
			return true
		}
	}
	return false
}

// applyTextSlotPadding configures heading bleed and body-text padding on text
// slots. On page-margin (outer) edges the heading color box bleeds outward
// to the page edge (headingBleed); body text reaches the slot edge unchanged.
// On interior edges where the adjacent slot is a photo, body text gets an
// inner padding (bodyTextPad mm) so it doesn't sit visually pressed against
// the photo; the heading color box compensates with the same value as bleed
// so its visual edge still aligns with the slot edge (= the photo edge).
// Interior edges adjacent to non-photo slots (text/captions/empty) get no
// padding — only the photo-adjacent side breathes.
func applyTextSlotPadding(slots []TemplateSlot, format string, headingBleed, bodyTextPad float64) {
	for i := range slots {
		if !slots[i].HasText {
			continue
		}
		if isSlotOnLeftEdge(format, i) {
			slots[i].BleedLeftMM = headingBleed
		} else if anyNeighborIsPhoto(slots, leftNeighbors(format, i)) {
			slots[i].TextPadLeft = bodyTextPad
			slots[i].BleedLeftMM = bodyTextPad
		}
		if isSlotOnRightEdge(format, i) {
			slots[i].BleedRightMM = headingBleed
		} else if anyNeighborIsPhoto(slots, rightNeighbors(format, i)) {
			slots[i].TextPadRight = bodyTextPad
			slots[i].BleedRightMM = bodyTextPad
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
	return downloadPhotosWithProgress(ctx, pp, uids, tmpDir, nil, QualityMedium)
}

// downloadPhotosWithProgress fetches photos concurrently, reporting progress
// via onProgress after each photo (whether it succeeded or failed) so the
// progress bar reaches 100% even if some photos fail. onProgress may be nil.
// quality selects the resolution tier (see PhotoQuality).
func downloadPhotosWithProgress(
	ctx context.Context, pp *photoprism.PhotoPrism,
	uids map[string]bool, tmpDir string,
	onProgress func(ProgressInfo),
	quality PhotoQuality,
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
			downloadOnePhoto(pp, uid, tmpDir, result, &mu, quality)
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
	workerCount := downloadConcurrency
	if normalizeQuality(quality) == QualityOriginal {
		workerCount = originalDownloadConcurrency
	}
	for range workerCount {
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
	quality PhotoQuality,
) {
	img, err := downloadPhoto(pp, uid, tmpDir, quality)
	if err != nil {
		log.Printf("WARNING: failed to download photo %s: %v", uid, err)
		return
	}
	mu.Lock()
	result[uid] = *img
	mu.Unlock()
}

// downloadPhoto fetches a single photo at the requested quality tier and
// returns its path and dimensions on disk. The returned file is always a
// decodable image (JPEG or equivalent) so lualatex can embed it.
func downloadPhoto(
	pp *photoprism.PhotoPrism, uid string, tmpDir string, quality PhotoQuality,
) (*photoImage, error) {
	photos, err := pp.GetPhotosWithQuery(1, 0, "uid:"+uid)
	if err != nil || len(photos) == 0 {
		return nil, fmt.Errorf("photo not found: %s", uid)
	}
	hash := photos[0].Hash
	if hash == "" {
		return nil, fmt.Errorf("photo has no hash: %s", uid)
	}

	if normalizeQuality(quality) == QualityOriginal {
		return downloadOriginalPhoto(pp, uid, hash, tmpDir)
	}

	size := thumbnailMedium
	if normalizeQuality(quality) == QualityLow {
		size = thumbnailLow
	}
	return downloadThumbnailPhoto(pp, uid, hash, tmpDir, size)
}

// downloadThumbnailPhoto fetches a single PhotoPrism thumbnail at the given
// size and returns its path and dimensions.
func downloadThumbnailPhoto(
	pp *photoprism.PhotoPrism, uid, hash, tmpDir, size string,
) (*photoImage, error) {
	data, _, err := pp.GetPhotoThumbnail(hash, size)
	if err != nil {
		return nil, fmt.Errorf("failed to download thumbnail: %w", err)
	}

	path := filepath.Join(tmpDir, uid+".jpg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write photo: %w", err)
	}

	return inspectPhotoFile(path, uid)
}

// downloadOriginalPhoto fetches the original primary file, converts it to
// JPEG if necessary, and caps the longest side at OriginalMaxLongestSidePx.
// If the primary file cannot be decoded in pure Go (HEIC / RAW formats), the
// function falls back to PhotoPrism's largest pre-rendered JPEG thumbnail
// (fit_7680) so the PDF still gets a print-quality image.
//
// The implementation streams the response body directly to a temp file
// (avoiding a full []byte allocation), then sniffs the format and dimensions
// via image.DecodeConfig. If the source is already a JPEG within the size
// cap, it is renamed to the final path without ever decoding pixel data —
// this is the common case for typical photo libraries and is what keeps the
// resident-memory peak bounded for ~200-page books in QualityOriginal.
func downloadOriginalPhoto(
	pp *photoprism.PhotoPrism, uid, hash, tmpDir string,
) (*photoImage, error) {
	finalPath := filepath.Join(tmpDir, uid+".jpg")
	tmpPath := filepath.Join(tmpDir, uid+".orig")

	if err := streamOriginalToFile(pp, uid, tmpPath); err != nil {
		return nil, err
	}
	// Removed on every exit path (slow path renames-then-removes; fallback
	// branches return without consuming the temp file).
	defer func() { _ = os.Remove(tmpPath) }()

	cfg, format, sniffErr := decodeConfigFile(tmpPath)
	if sniffErr != nil {
		sanitizedUID := strings.NewReplacer("\n", "", "\r", "").Replace(uid)
		log.Printf(
			"PDF export: photo %s original format not decodable (%v); "+
				"falling back to %s thumbnail",
			sanitizedUID, sniffErr, thumbnailOriginalFallback,
		)
		return downloadThumbnailPhoto(pp, uid, hash, tmpDir, thumbnailOriginalFallback)
	}

	longest := max(cfg.Width, cfg.Height)

	// Fast path: source is JPEG and within the size cap. Move the streamed
	// temp file to its final name and skip decode/re-encode entirely.
	if format == "jpeg" && longest <= OriginalMaxLongestSidePx {
		if err := os.Rename(tmpPath, finalPath); err != nil {
			return nil, fmt.Errorf("failed to rename original: %w", err)
		}
		sanitizedUID := strings.NewReplacer("\n", "", "\r", "").Replace(uid)
		log.Printf("PDF export: photo %s dimensions %dx%d (pass-through)",
			sanitizedUID, cfg.Width, cfg.Height)
		return &photoImage{path: finalPath, width: cfg.Width, height: cfg.Height}, nil
	}

	return reencodeOriginalSlowPath(pp, uid, hash, tmpDir, tmpPath, finalPath, longest)
}

// reencodeOriginalSlowPath handles the non-JPEG / oversized-JPEG branch of
// downloadOriginalPhoto: full decode, optional resize to OriginalMaxLongestSidePx,
// JPEG re-encode at originalJPEGQuality. On decode failure it falls back to the
// pre-rendered fit_7680 thumbnail.
func reencodeOriginalSlowPath(
	pp *photoprism.PhotoPrism, uid, hash, tmpDir, tmpPath, finalPath string, longest int,
) (*photoImage, error) {
	img, err := decodeImageFile(tmpPath)
	if err != nil {
		sanitizedUID := strings.NewReplacer("\n", "", "\r", "").Replace(uid)
		log.Printf(
			"PDF export: photo %s decode failed after sniff (%v); "+
				"falling back to %s thumbnail",
			sanitizedUID, err, thumbnailOriginalFallback,
		)
		return downloadThumbnailPhoto(pp, uid, hash, tmpDir, thumbnailOriginalFallback)
	}

	if longest > OriginalMaxLongestSidePx {
		img = resizeToLongestSide(img, OriginalMaxLongestSidePx)
	}

	f, err := os.OpenFile(finalPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to create photo file: %w", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: originalJPEGQuality}); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to encode original as JPEG: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close photo file: %w", err)
	}

	return inspectPhotoFile(finalPath, uid)
}

// streamOriginalToFile pipes the photo's primary-file body straight to disk
// without buffering the whole payload in RAM.
func streamOriginalToFile(pp *photoprism.PhotoPrism, uid, dst string) error {
	body, _, err := pp.GetPhotoDownloadStream(uid)
	if err != nil {
		return fmt.Errorf("failed to download original: %w", err)
	}
	defer body.Close()

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to create temp original: %w", err)
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to stream original: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp original: %w", err)
	}
	return nil
}

// decodeConfigFile returns image.DecodeConfig output (header-only metadata)
// for the file at path. It does not allocate a pixel buffer.
func decodeConfigFile(path string) (image.Config, string, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return image.Config{}, "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("decode config: %w", err)
	}
	return cfg, format, nil
}

// decodeImageFile fully decodes the image at path into an image.Image.
func decodeImageFile(path string) (image.Image, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return img, nil
}

// resizeToLongestSide downscales img so its longest side equals maxPx while
// preserving aspect ratio. Uses CatmullRom for good print quality.
func resizeToLongestSide(img image.Image, maxPx int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	var newW, newH int
	if w >= h {
		newW = maxPx
		newH = int(float64(h) * float64(maxPx) / float64(w))
	} else {
		newH = maxPx
		newW = int(float64(w) * float64(maxPx) / float64(h))
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

// inspectPhotoFile opens the written file, decodes its dimensions, and
// returns a populated photoImage.
func inspectPhotoFile(path, uid string) (*photoImage, error) {
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

// ContentsHeaderText is the headline rendered at the top of a contents slot.
// It is intentionally hard-coded in Czech until per-book localisation lands.
const ContentsHeaderText = "Obsah"

// BuildBookTOC computes the table of contents for a book directly from the
// repository, without triggering photo downloads or LaTeX rendering. It is
// used by the single-page preview path (and other cheap readers) to produce
// the same TOC the full book export would emit for a contents slot.
//
// Page numbers are computed as the 1-based position of each page in the
// canonical (section_sort_order, page_sort_order) ordering used by the full
// export — the same rule that drives pageBuilder.pageNumber.
func BuildBookTOC(ctx context.Context, br database.BookReader, bookID string) ([]TOCChapter, error) {
	sections, err := br.GetSections(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("toc: get sections: %w", err)
	}
	chapters, err := br.GetChapters(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("toc: get chapters: %w", err)
	}
	pages, err := br.GetPages(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("toc: get pages: %w", err)
	}
	SortPagesBySectionOrder(pages, sections)
	groups := groupPagesBySection(pages, sections, chapters)

	ranges := make(map[string][2]int, len(groups))
	pageNumber := 0
	for _, g := range groups {
		startPage := pageNumber + 1
		for range g.pages {
			pageNumber++
		}
		if len(g.pages) > 0 && g.sectionID != "" {
			ranges[g.sectionID] = [2]int{startPage, pageNumber}
		}
	}
	return buildTOCData(groups, ranges), nil
}

// buildContentsSlotStub creates a TemplateSlot marker for a contents (table
// of contents) slot. ContentsEntries is left empty and is populated in a
// second pass by injectContentsSlots once all page numbers are known.
func buildContentsSlotStub(
	slot SlotRect, contentLeftX, canvasTopY float64,
) TemplateSlot {
	return TemplateSlot{
		HasContents:    true,
		ClipX:          contentLeftX + slot.X,
		ClipY:          canvasTopY - slot.Y - slot.H,
		ClipW:          slot.W,
		ClipH:          slot.H,
		ContentsHeader: ContentsHeaderText,
	}
}

// buildTOCData builds the book's table of contents from the section page
// ranges collected during pageBuilder.buildSection. Chapters appear in the
// order their first section is encountered in the book (which mirrors the
// user-configured chapter + section sort order). Sections without a chapter
// are grouped under a single chapter entry with an empty title.
//
// Empty-page sections (no printed pages) are skipped; a section without an
// entry in sectionPageRanges contributes nothing to the TOC. Chapters
// flagged as HideFromTOC are omitted entirely — their pages still render
// normally, they just do not appear in the TOC listing.
//
// The returned slice is annotated via balanceTOCColumns so the LaTeX
// template can place a \columnbreak before the chapter that should open
// the right column.
func buildTOCData(groups []sectionGroup, sectionPageRanges map[string][2]int) []TOCChapter {
	chapterIndex := make(map[string]int)
	var toc []TOCChapter
	for _, g := range groups {
		if g.chapterHideFromTOC {
			continue
		}
		rng, ok := sectionPageRanges[g.sectionID]
		if !ok {
			continue
		}
		idx, seen := chapterIndex[g.chapterID]
		if !seen {
			idx = len(toc)
			chapterIndex[g.chapterID] = idx
			toc = append(toc, TOCChapter{Title: g.chapterTitle})
		}
		toc[idx].Sections = append(toc[idx].Sections, TOCSection{
			Title:     g.title,
			StartPage: rng[0],
			EndPage:   rng[1],
		})
	}
	balanceTOCColumns(toc)
	return toc
}

// balanceTOCColumns picks the chapter boundary that best balances a two-column
// TOC layout and flags that chapter's StartsRightColumn = true so the template
// can emit a \columnbreak before it. "Best" means the split that minimises
// the absolute difference between column row counts (chapter title row +
// one row per section). With fewer than two chapters the TOC trivially fits
// in the first column and no break is needed.
func balanceTOCColumns(toc []TOCChapter) {
	if len(toc) < 2 {
		return
	}
	rows := make([]int, len(toc))
	total := 0
	for i, ch := range toc {
		r := len(ch.Sections)
		if ch.Title != "" {
			r++
		}
		rows[i] = r
		total += r
	}
	bestIdx := -1
	bestDiff := total + 1 // any real diff beats this
	left := 0
	// i iterates over possible break points: the right column starts at
	// chapter (i+1). We do NOT consider breaking before the first chapter
	// or after the last — both leave an empty column.
	for i := range len(toc) - 1 {
		left += rows[i]
		right := total - left
		diff := left - right
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			bestIdx = i + 1
		}
	}
	if bestIdx > 0 {
		toc[bestIdx].StartsRightColumn = true
	}
}

// injectContentsSlots walks every page's slots and fills in the TOC entries
// for any slot flagged HasContents. Called once all sections have been built
// so page ranges are final.
func injectContentsSlots(sections []TemplateSection, toc []TOCChapter) {
	for si := range sections {
		pages := sections[si].Pages
		for pi := range pages {
			slots := pages[pi].Slots
			for i := range slots {
				if !slots[i].HasContents {
					continue
				}
				slots[i].ContentsEntries = toc
				if slots[i].ContentsHeader == "" {
					slots[i].ContentsHeader = ContentsHeaderText
				}
			}
		}
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
	// TOC is the book's full table of contents used when the rendered page
	// contains a contents slot. The caller is responsible for computing it
	// (see BuildBookTOC) when the preview should match the book export. May
	// be nil when the page has no contents slot.
	TOC []TOCChapter
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
	typo := resolveBookTypography(input.Book)

	tmplPage := buildSinglePage(input, photos, typo)
	sections := []TemplateSection{{Pages: []TemplatePage{tmplPage}}}
	// If the page contains a contents slot, fill it with the pre-computed
	// book TOC so the preview matches what full book export would render.
	injectContentsSlots(sections, input.TOC)

	data := singlePageTemplateData(sections, typo)

	pdfData, err := compileLatex(ctx, data, tmpDir)
	if err != nil {
		return nil, err
	}
	return pdfData, nil
}

// buildSinglePage drives a pageBuilder to produce exactly one TemplatePage
// at the page number indicated by input.PageNumber.
func buildSinglePage(
	input SinglePageInput, photos map[string]photoImage, typo resolvedTypography,
) TemplatePage {
	pageNum := max(input.PageNumber, 1)
	pb := &pageBuilder{
		config:            DefaultLayoutConfig(),
		photos:            photos,
		captions:          input.Captions,
		totalContentPages: pageNum,
		contentPageIdx:    pageNum - 1,
		pageNumber:        pageNum - 1, // incremented to pageNum below
		photoSet:          make(map[string]bool),
		headingColorBleed: typo.headingColorBleed,
		captionBadgeSize:  typo.captionBadgeSize,
		bodyTextPadMM:     typo.bodyTextPadMM,
	}
	pb.contentPageIdx++
	pb.pageNumber++
	return pb.buildContentPage(input.Page, input.ChapterColor)
}

// singlePageTemplateData assembles TemplateData for a single-page render,
// applying the already-resolved book typography.
func singlePageTemplateData(sections []TemplateSection, typo resolvedTypography) TemplateData {
	return TemplateData{
		Sections:               sections,
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
}
