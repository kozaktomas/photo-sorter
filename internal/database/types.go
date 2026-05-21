package database

import (
	"errors"
	"time"
)

// ErrNotFound is returned by single-row lookup repository methods when the
// requested record does not exist.
var ErrNotFound = errors.New("record not found")

// ErrCaptionsSlotExists is returned by AssignCaptionsSlot when the target page
// already has a different slot marked as its captions slot. At most one
// captions slot is allowed per page.
var ErrCaptionsSlotExists = errors.New("page already has a captions slot")

// ErrContentsSlotExists is returned by AssignContentsSlot when the target page
// already has a different slot marked as its contents slot. At most one
// contents slot is allowed per page.
var ErrContentsSlotExists = errors.New("page already has a contents slot")

// ErrSectionNotFound is returned when a referenced section ID does not exist.
var ErrSectionNotFound = errors.New("section not found")

// ErrPageNotFound is returned when a referenced page ID does not exist.
var ErrPageNotFound = errors.New("page not found")

// ErrSectionBookMismatch is returned by MovePageToSection when the target
// section belongs to a different book than the page being moved.
var ErrSectionBookMismatch = errors.New("target section belongs to a different book")

// ErrUsernameTaken is returned by UserWriter.CreateUser when a user with the
// same username already exists. It maps the underlying PostgreSQL
// unique_violation (SQLSTATE 23505) on the users.username column into a
// typed error that callers can match with errors.Is.
var ErrUsernameTaken = errors.New("username already taken")

// User represents an account in the native user store. UID format is
// `"u" + 16 random base32 lowercase chars`. PasswordHash is intentionally
// not part of this struct — see UserWithSecret for the variant that carries
// the hash for the login flow.
type User struct {
	UID         string
	Username    string
	DisplayName string
	Email       string
	Role        string
	Disabled    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt *time.Time
}

// UserWithSecret is User extended with the bcrypt password hash. Only the
// login flow and the user creation path should touch this struct; never
// serialise it to any API response or log line.
type UserWithSecret struct {
	User
	PasswordHash string
}

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
	ID          string
	BookID      string
	Title       string
	Color       string
	HideFromTOC bool // true = skip this chapter (and its sections) in the auto-generated TOC
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
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

// PageSlot represents a photo, text, captions, or contents assignment to a
// position on a page. A slot holds at most one of PhotoUID, TextContent,
// IsCaptionsSlot, or IsContentsSlot.
type PageSlot struct {
	SlotIndex      int
	PhotoUID       string  // empty = unoccupied (photo)
	TextContent    string  // non-empty = text slot
	IsCaptionsSlot bool    // true = render page's photo captions inline
	IsContentsSlot bool    // true = render auto-generated book table of contents
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

// IsContents returns true if this slot renders the book's table of contents.
func (s PageSlot) IsContents() bool {
	return s.IsContentsSlot
}

// IsEmpty returns true if the slot has no photo, text, captions, or contents.
func (s PageSlot) IsEmpty() bool {
	return s.PhotoUID == "" && s.TextContent == "" && !s.IsCaptionsSlot && !s.IsContentsSlot
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

// Photo represents a single photo managed by the native photo pipeline.
// It mirrors the columns of the photos table introduced in migration 032.
type Photo struct {
	UID             string
	FileHash        string
	FilePath        string
	FileName        string
	FileSize        int64
	FileMime        string
	FileWidth       int
	FileHeight      int
	FileOrientation int
	TakenAt         *time.Time
	TakenAtSource   string
	Title           string
	Description     string
	Notes           string
	Lat             *float64
	Lng             *float64
	Altitude        *float64
	CameraMake      string
	CameraModel     string
	LensModel       string
	ISO             *int
	Aperture        *float64
	Exposure        string
	FocalLength     *float64
	Exif            map[string]any
	Favorite        bool
	Private         bool
	ArchivedAt      *time.Time
	UploadedBy      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PhotoFile represents a single physical file belonging to a Photo
// (a stack — original + sidecar + edited variants).
type PhotoFile struct {
	ID        int64
	PhotoUID  string
	FilePath  string
	FileHash  string
	FileSize  int64
	FileMime  string
	IsPrimary bool
	Role      string
	CreatedAt time.Time
}

// BBox is an optional latitude/longitude bounding box used for spatial
// filtering of photos.
type BBox struct {
	MinLat float64
	MinLng float64
	MaxLat float64
	MaxLng float64
}

// PhotoFilter holds optional filter and pagination criteria for ListPhotos.
// All fields are optional; the zero value lists non-archived photos sorted
// by newest first with the default page size.
type PhotoFilter struct {
	AlbumUID    string   // empty = any album
	LabelUIDs   []string // AND semantics: photo must have all labels
	SubjectUIDs []string // AND semantics: photo must have markers for all subjects
	Favorite    *bool
	Private     *bool
	Archived    *bool // nil = exclude archived; *true = only archived; *false = explicit non-archived
	TakenFrom   *time.Time
	TakenTo     *time.Time
	BBox        *BBox
	UploadedBy  string
	Search      string // ILIKE match against title/description/file_name
	SortBy      string // "newest" (default) / "oldest" / "name"
	Limit       int    // 0 = default 50, capped at 500
	Offset      int
}

// Album is a curated or auto-generated grouping of photos. Mirrors the
// columns of the albums table introduced in migration 032. PhotoCount is
// computed at query time (it is not a stored column).
type Album struct {
	UID           string
	Slug          string
	Title         string
	Description   string
	Type          string // album / folder / moment / state / month
	CoverPhotoUID string
	Favorite      bool
	Private       bool
	OrderBy       string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	PhotoCount    int
}

// AlbumPhoto is a single row of the album_photos junction table. SortOrder
// gives the explicit ordering of photos within the album.
type AlbumPhoto struct {
	AlbumUID  string
	PhotoUID  string
	SortOrder int
	AddedAt   time.Time
}

// AlbumQuery holds optional filter and pagination criteria for AlbumReader.ListAlbums.
type AlbumQuery struct {
	Type     string
	Favorite *bool
	Search   string
	SortBy   string // "title" / "newest" / "oldest" / "photos"
	Limit    int
	Offset   int
}

// ErrAlbumPhotoNotInAlbum is returned by AlbumWriter.SetCoverPhoto when the
// caller tries to set a cover photo that is not a member of the album.
var ErrAlbumPhotoNotInAlbum = errors.New("photo is not in album")

// Label is a tag/category that can be applied to photos. Mirrors the columns
// of the labels table introduced in migration 032. PhotoCount is computed at
// query time (it is not a stored column).
type Label struct {
	UID        string
	Slug       string
	Name       string
	Priority   int
	Favorite   bool
	PhotoCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// LabelQuery holds optional filter and pagination criteria for
// LabelReader.ListLabels.
type LabelQuery struct {
	MinPhotos int
	Search    string
	SortBy    string // "name" / "-name" / "count" / "-count"
	Limit     int
	Offset    int
}

// Marker is a face or label region on a photo. Coordinates are relative
// (0..1) in display space. SubjectUID is empty when the marker is not yet
// assigned to a subject; the FK on the underlying column is ON DELETE SET
// NULL, so deleting a subject leaves its markers in place but unassigned.
type Marker struct {
	UID        string
	PhotoUID   string
	SubjectUID string  // empty when unassigned
	Type       string  // "face" or "label"
	X          float64 // top-left x (0..1, display space)
	Y          float64 // top-left y (0..1, display space)
	W          float64 // width (0..1, display space)
	H          float64 // height (0..1, display space)
	Score      int     // detection score (0..100)
	Invalid    bool    // operator-marked false positive
	Reviewed   bool    // operator-reviewed
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Subject is a recurring face/label target (a person, pet, or other named
// entity). PhotoCount and FaceCount are computed at query time from the
// markers table (both exclude markers flagged invalid).
type Subject struct {
	UID           string
	Slug          string
	Name          string
	Type          string // "person" / "pet" / "other"
	Favorite      bool
	Private       bool
	Notes         string
	CoverPhotoUID string
	PhotoCount    int // computed: COUNT(DISTINCT photo_uid) of valid markers
	FaceCount     int // computed: COUNT(*) of valid markers
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SubjectQuery holds optional filter and pagination criteria for
// SubjectReader.ListSubjects.
type SubjectQuery struct {
	Type     string
	Favorite *bool
	Search   string
	SortBy   string // "name" / "photos" / "newest"
	Limit    int
	Offset   int
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
