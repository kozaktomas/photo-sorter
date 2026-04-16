package database

import (
	"errors"
	"time"
)

// ErrCaptionsSlotExists is returned by AssignCaptionsSlot when the target page
// already has a different slot marked as its captions slot. At most one
// captions slot is allowed per page.
var ErrCaptionsSlotExists = errors.New("page already has a captions slot")

// StoredEmbedding represents an embedding stored in the database.
type StoredEmbedding struct {
	PhotoUID   string
	Embedding  []float32
	Model      string
	Pretrained string
	Dim        int
	CreatedAt  time.Time
}

// StoredFace represents a face embedding stored in the database.
type StoredFace struct {
	ID        int64
	PhotoUID  string
	FaceIndex int
	Embedding []float32
	BBox      []float64 // [x1, y1, x2, y2] in raw pixel coordinates
	DetScore  float64
	Model     string
	Dim       int
	CreatedAt time.Time

	// Cached PhotoPrism data (populated during processing, v3+).
	MarkerUID   string // Matching PhotoPrism marker UID (empty if no marker matched)
	SubjectUID  string // Subject UID from marker (empty if unassigned)
	SubjectName string // Person name from marker (empty if unassigned)
	PhotoWidth  int    // Primary file width in pixels
	PhotoHeight int    // Primary file height in pixels
	Orientation int    // EXIF orientation (1-8)
	FileUID     string // Primary file UID
}

// FaceProcessedRecord represents a record of a photo that has been processed for face detection.
type FaceProcessedRecord struct {
	PhotoUID  string
	FaceCount int
	CreatedAt time.Time
}

// StoredEraEmbedding represents a CLIP text embedding centroid for a photo era.
type StoredEraEmbedding struct {
	EraSlug            string
	EraName            string
	RepresentativeDate string // "YYYY-MM-DD"
	PromptCount        int
	Embedding          []float32
	Model              string
	Pretrained         string
	Dim                int
	CreatedAt          time.Time
}

// ExportData contains all embeddings and faces data for export/storage.
type ExportData struct {
	Version        int
	ExportedAt     time.Time
	Embeddings     []StoredEmbedding
	Faces          []StoredFace
	FacesProcessed []FaceProcessedRecord // Photos processed for face detection (v2+)
}

// PhotoBook represents a photo book project.
type PhotoBook struct {
	ID                string
	Title             string
	Description       string
	BodyFont          string
	HeadingFont       string
	BodyFontSize      float64
	BodyLineHeight    float64
	H1FontSize        float64
	H2FontSize        float64
	CaptionOpacity    float64
	CaptionFontSize   float64
	HeadingColorBleed float64
	CaptionBadgeSize  float64
	BodyTextPadMM     float64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PhotoBookWithCounts extends PhotoBook with precomputed counts for list views.
type PhotoBookWithCounts struct {
	PhotoBook
	SectionCount int
	PageCount    int
	PhotoCount   int
}

// BookChapter represents a chapter grouping within a book.
type BookChapter struct {
	ID        string
	BookID    string
	Title     string
	Color     string
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BookSection represents an ordered group within a book.
type BookSection struct {
	ID         string
	BookID     string
	ChapterID  string // empty string = no chapter
	Title      string
	SortOrder  int
	PhotoCount int // computed, not stored
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SectionPhoto represents a photo in a section's prepick pool.
type SectionPhoto struct {
	ID          int64
	SectionID   string
	PhotoUID    string
	Description string
	Note        string
	AddedAt     time.Time
}

// BookPage represents a page with a specific format.
type BookPage struct {
	ID             string
	BookID         string
	SectionID      string // optional, may be empty
	Format         string
	Style          string // "modern" or "archival"
	Description    string
	SplitPosition  *float64 // nullable; 0.2-0.8 column ratio; nil = format default
	HidePageNumber bool     // suppress folio rendering on this page (numbering continues)
	SortOrder      int
	Slots          []PageSlot // populated on read
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PageSlot represents a photo, text, or captions assignment to a position on
// a page. A slot holds at most one of PhotoUID, TextContent, or IsCaptionsSlot.
type PageSlot struct {
	SlotIndex      int
	PhotoUID       string  // empty = unoccupied (photo)
	TextContent    string  // non-empty = text slot
	IsCaptionsSlot bool    // true = render page's photo captions inline
	CropX          float64 // 0.0-1.0, default 0.5 (center)
	CropY          float64 // 0.0-1.0, default 0.5 (center)
	CropScale      float64 // 0.1-1.0, default 1.0 (no zoom)
}

// IsTextSlot returns true if this slot contains text content (no photo).
func (s PageSlot) IsTextSlot() bool {
	return s.TextContent != "" && s.PhotoUID == ""
}

// IsCaptions returns true if this slot renders the page's photo captions.
func (s PageSlot) IsCaptions() bool {
	return s.IsCaptionsSlot
}

// IsEmpty returns true if the slot has neither a photo, text, nor captions.
func (s PageSlot) IsEmpty() bool {
	return s.PhotoUID == "" && s.TextContent == "" && !s.IsCaptionsSlot
}

// TextVersion stores a historical snapshot of a text field.
type TextVersion struct {
	ID         int
	SourceType string // "section_photo" or "page_slot"
	SourceID   string // "sectionID:photoUID" or "pageID:slotIndex"
	Field      string // "description", "note", or "text_content"
	Content    string
	ChangedBy  string // "user" or "ai"
	CreatedAt  time.Time
}

// TextSuggestion is an advisory readability recommendation stored with
// a text check result (e.g. "sentence is too long", "repeated word").
type TextSuggestion struct {
	Severity string `json:"severity"` // "major" or "minor"
	Message  string `json:"message"`
}

// TextCheckResult stores the result of an AI text check for a specific text field.
type TextCheckResult struct {
	ID               int
	SourceType       string           // "section_photo" or "page_slot"
	SourceID         string           // "sectionID:photoUID" or "pageID:slotIndex"
	Field            string           // "description", "note", or "text_content"
	ContentHash      string           // SHA-256 of the text that was checked
	Status           string           // "clean" or "has_errors"
	ReadabilityScore *int             // 0-100, nil if not applicable
	CorrectedText    string           // corrected version (if errors found)
	Changes          []string         // array of mechanical change descriptions
	Suggestions      []TextSuggestion // advisory readability recommendations
	CostCZK          float64          // cost of the check
	CheckedAt        time.Time        // when the check was performed
}

// PhotoBookMembership represents a book+section that contains a photo.
type PhotoBookMembership struct {
	BookID       string
	BookTitle    string
	SectionID    string
	SectionTitle string
}

// DefaultSplitPosition returns the default left-column fraction for a format.
func DefaultSplitPosition(format string) float64 {
	switch format {
	case "2l_1p":
		return 2.0 / 3.0
	case "1p_2l":
		return 1.0 / 3.0
	default:
		return 0.5
	}
}

// PageFormatSlotCount returns the number of slots for a given page format.
func PageFormatSlotCount(format string) int {
	switch format {
	case "4_landscape":
		return 4
	case "2l_1p":
		return 3
	case "1p_2l":
		return 3
	case "2_portrait":
		return 2
	case "1_fullscreen":
		return 1
	case "1_fullbleed":
		return 1
	default:
		return 0
	}
}
