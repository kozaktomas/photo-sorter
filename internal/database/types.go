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
// It mirrors the columns of the photos table introduced in migration 032
// and extended by migration 036 with the metadata fields that previously
// fell on the floor during migrate-from-photoprism.
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
	TimeZone        string // IANA name (e.g. "Europe/Prague"); empty when unknown
	TakenAtOffset   int    // seconds east of UTC for the TakenAt instant
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
	ExifArtist      string
	ExifCopyright   string
	ExifLicense     string
	ExifSoftware    string
	Keywords        []string
	Panorama        bool
	Scan            bool
	Quality         int16 // PhotoPrism quality tier, clamped 0..7
	Favorite        bool
	Private         bool
	ArchivedAt      *time.Time
	UploadedBy      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PhotoPHash is the perceptual-hash row for a single photo. PHash/DHash are
// 64-bit hashes (uint64) computed by internal/fingerprint over the decoded
// image. They live in their own table (rather than on photos) so the hot
// photos row stays narrow and the hash backfill can run independently of
// the upload pipeline.
type PhotoPHash struct {
	PhotoUID  string
	PHash     uint64
	DHash     uint64
	CreatedAt time.Time
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

// HistogramBucket is a single bin in a photo-count histogram. Start is the
// inclusive lower bound and End is the exclusive upper bound of the bucket
// (so consecutive buckets tile the date range with no gap or overlap).
type HistogramBucket struct {
	Start time.Time
	End   time.Time
	Count int
}

// HistogramResult is the response shape for date-histogram queries. Buckets
// only span months/years that contain at least one matching photo (empty
// months in the middle of the range are filled by the caller if needed).
// NoDateCount counts photos that pass every other filter but have a NULL
// taken_at; NoGPSCount is the same for photos with NULL lat/lng. These two
// counts power the "No date / No location" chips on the Browse page.
type HistogramResult struct {
	Buckets     []HistogramBucket
	Total       int
	NoDateCount int
	NoGPSCount  int
}

// GeoPoint is a single photo's coordinates as returned by ListGeoPoints.
// Only photos with non-NULL lat/lng are returned, so the Lat/Lng fields are
// always concrete float64s.
type GeoPoint struct {
	PhotoUID string
	Lat      float64
	Lng      float64
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
	// Search drives the full-text (tsvector) match against
	// title/description/notes/file_name; results are re-ranked by
	// ts_rank when this is non-empty. Queries with no tokens of length
	// >= 2 fall back to a prefix ILIKE on title.
	Search string
	// UpdatedSince restricts the result to photos whose updated_at is >= the
	// given instant. Combined with SortUpdated + Cursor it drives the
	// incremental export used by the migration client.
	//
	// Caveat worth knowing: photos.updated_at is bumped by writes to the
	// photo row itself (metadata edits, archive, restore). Relation-only
	// changes — attaching a label, adding the photo to an album, moving a
	// face marker — do NOT touch it, so they are invisible to an
	// updated_since sweep. A relation-complete sync must do a full walk.
	UpdatedSince *time.Time
	SortBy       string // "newest" (default) / "oldest" / "name" / "updated"
	// Cursor is the keyset resume position and is only honoured together
	// with SortUpdated. Unlike every other field here it is NOT part of the
	// shared WHERE clause built by buildPhotoFilter: it is applied by
	// ListPhotos alone, so that (a) the reported total stays the size of the
	// whole matching set rather than shrinking each page, and (b) a cursor
	// cannot leak into the histogram / geo-points views that reuse the same
	// filter and would be silently truncated by it.
	Cursor *PhotoCursor
	Limit  int // 0 = default 50, capped at 500
	Offset int
}

// SortUpdated is the sort key that orders photos by (updated_at, uid)
// ascending. It is the only ordering a PhotoCursor is valid against: the
// cursor is a keyset over exactly that pair.
//
// Ascending is what makes an export resumable. Any write bumps updated_at to
// now(), which is ahead of every row already walked, so a photo modified
// mid-export re-appears later in the walk instead of being skipped. The
// client may therefore see a row twice (harmless — an export upserts by UID),
// but it can never miss one.
const SortUpdated = "updated"

// PhotoLabelRelation is a label attached to a photo, carrying the provenance
// columns from the photo_labels join row. Source is one of manual / ai /
// import; Uncertainty is PhotoPrism's 0..100 scale where 0 means "certain".
type PhotoLabelRelation struct {
	UID         string
	Name        string
	Source      string
	Uncertainty int
}

// PhotoAlbumRelation is the slim album reference returned by the ?include=
// expansion — enough for an importer to rebuild album membership without
// pulling every album column.
type PhotoAlbumRelation struct {
	UID   string
	Title string
}

// PhotoMarkerRelation is a face/label marker on a photo. Unlike the marker
// view served by GET /photos/{uid}/faces, this one exposes SubjectUID (the
// canonical person identity) rather than only the display name, so an
// importer can reconstruct marker→person links without name matching.
// X/Y/W/H are relative (0..1) display-space coordinates.
type PhotoMarkerRelation struct {
	UID        string
	SubjectUID string // empty when the marker is not assigned to a subject
	Type       string // face / label
	X, Y, W, H float64
	Score      int
	Invalid    bool
	Reviewed   bool
}

// PhotoRelations bundles every optional expansion for a single photo. A nil
// slice means the caller did not ask for that relation; an empty non-nil
// slice means it was asked for and the photo has none.
type PhotoRelations struct {
	Labels  []PhotoLabelRelation
	Albums  []PhotoAlbumRelation
	Markers []PhotoMarkerRelation
	Files   []PhotoFile
}

// Album is a curated or auto-generated grouping of photos. Mirrors the
// columns of the albums table introduced in migration 032 and extended by
// migration 037 with Location/Category/Notes/Filter/Order. PhotoCount is
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
	// Location is the free-form place string from PhotoPrism's album_location.
	Location string
	// Category is the PhotoPrism album_category column (free-form text).
	Category string
	// Notes mirrors PhotoPrism's album_notes (free-form, distinct from description).
	Notes string
	// Filter holds the raw smart-album DSL string from PhotoPrism's
	// album_filter column. photo-sorter has no smart-album evaluator yet;
	// the value is preserved verbatim so a future feature can consume it
	// and so the operator can audit which albums were smart-filtered.
	Filter string
	// Order is the PhotoPrism album_order sort-order setting (e.g. "oldest"
	// / "newest"). It is distinct from OrderBy (which is the native column
	// already used by the album list endpoints) and is preserved verbatim
	// so future smart-album features can honour the user's PhotoPrism
	// preference.
	Order      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	PhotoCount int
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
// of the labels table introduced in migration 032 and extended by migration
// 037 with Description/Categories. PhotoCount is computed at query time (it
// is not a stored column).
type Label struct {
	UID  string
	Slug string
	Name string
	// Description is PhotoPrism's label_description (free-form text).
	Description string
	// Categories mirrors PhotoPrism's label_categories. PhotoPrism stores
	// the value as a comma-separated string; the migrator unpacks that
	// into a deduplicated slice. The native column is TEXT[] so empty maps
	// to an empty slice, not nil-on-the-wire.
	Categories []string
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
// markers table (both exclude markers flagged invalid). Bio/About/Alias
// were added by migration 037 to plug PhotoPrism subj_bio / subj_about /
// subj_alias data loss during migration.
type Subject struct {
	UID  string
	Slug string
	Name string
	Type string // "person" / "pet" / "other"
	// Bio is the long-form biography string from PhotoPrism's subj_bio.
	Bio string
	// About is the short tagline from PhotoPrism's subj_about.
	About string
	// Alias holds the PhotoPrism subj_alias column verbatim. PhotoPrism
	// may store multiple aliases as a single comma-separated string;
	// the native schema preserves that exactly (no parsing).
	Alias         string
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

// ErrShareLinkExpired is returned when a public share link is past its
// expires_at timestamp. The public API translates this into HTTP 410.
var ErrShareLinkExpired = errors.New("share link expired")

// ErrShareLinkSlugTaken is returned by ShareLinkWriter.CreateShareLink when
// the requested slug is already in use. Slug uniqueness is enforced by the
// table's primary key; this typed error lets the handler return 409.
var ErrShareLinkSlugTaken = errors.New("share link slug already exists")

// ErrShareLinkInvalidSlug is returned when a requested slug fails the
// `^[a-z0-9-]{3,64}$` validation. The handler returns 400.
var ErrShareLinkInvalidSlug = errors.New("share link slug must match ^[a-z0-9-]{3,64}$")

// ShareLink is a public share link for an album. PasswordHash is the
// bcrypt-hashed password (NULL/empty when the link is unprotected). The
// raw password is never persisted. ExpiresAt is the optional expiration
// timestamp (NULL = no expiration).
type ShareLink struct {
	Slug             string
	AlbumUID         string
	PasswordHash     string // bcrypt hash; empty when no password
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	CreatedByUserUID string
}

// HasPassword returns true when the link is password-protected.
func (l *ShareLink) HasPassword() bool {
	return l.PasswordHash != ""
}

// IsExpired returns true when the link has a non-NULL expires_at that is
// at or before the supplied now.
func (l *ShareLink) IsExpired(now time.Time) bool {
	if l.ExpiresAt == nil {
		return false
	}
	return !now.Before(*l.ExpiresAt)
}

// SmartAlbum is a saved photo search. The Filters map carries the same
// query-param grammar accepted by `GET /api/v1/photos` (label_uids,
// subject_uids, favorite, taken_from, taken_to, the geo bbox, q, sort) as a
// JSONB blob — the web/MCP handler validates the shape on write and re-
// evaluates the query live on read, so a smart album always reflects the
// current library state. The repository serialises Filters via
// encoding/json before INSERTing the row.
type SmartAlbum struct {
	UID              string
	Name             string
	Filters          map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CreatedByUserUID string
}

// PhotoEdits holds the non-destructive edit parameters stored in the
// photo_edits table. A row exists only when at least one field is at a
// non-default value; the row is deleted when the user reverts the photo
// to its original. Crop is nil when no crop is applied; the four crop
// floats are forwarded to/from the database as a single NULL group via
// the CHECK constraint added in migration 041.
//
// Coordinates are 0.0–1.0 relative to the rotated (display-oriented)
// image. Rotation is restricted to multiples of 90°.  Brightness and
// contrast are in [-1.0, 1.0] with 0 = no change.
type PhotoEdits struct {
	PhotoUID   string
	Crop       *PhotoEditsCrop
	Rotation   int
	Brightness float64
	Contrast   float64
	UpdatedAt  time.Time
}

// PhotoEditsCrop is the crop sub-struct of PhotoEdits. All four fields
// are required when present and live in [0.0, 1.0].
type PhotoEditsCrop struct {
	X float64
	Y float64
	W float64
	H float64
}

// AuditLogEntry is one row from the audit_log table. UserUID is empty
// when the action was performed anonymously (e.g. a failed login attempt)
// or when the original actor has since been deleted (ON DELETE SET NULL).
// EntityType/EntityUID identify the affected object when applicable —
// they may both be empty (e.g. an account-level password change). Metadata
// is opaque action-specific JSON; the handler decodes it for display.
type AuditLogEntry struct {
	ID         int64
	UserUID    string
	Username   string
	Action     string
	EntityType string
	EntityUID  string
	Metadata   map[string]any
	IP         string
	UserAgent  string
	CreatedAt  time.Time
}

// AuditLogFilter captures the query parameters accepted by the
// GET /api/v1/audit-log endpoint. Empty/zero values mean "no filter on
// this dimension"; Limit/Offset are mandatory and validated by the
// handler before being passed in.
type AuditLogFilter struct {
	UserUID    string
	Action     string
	EntityType string
	EntityUID  string
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
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

// APIToken is a long-lived bearer credential for a machine client (the
// migration exporter). Only the SHA-256 of the raw token is persisted, so
// TokenHash — never the token itself — is what round-trips through this
// struct. The raw value is returned exactly once, by the code that mints it.
//
// Scope is 'read' for every token today; the auth path maps a token onto the
// viewer role and additionally rejects any non-safe HTTP method, so a token
// cannot write regardless of what the column says.
type APIToken struct {
	UID              string
	Name             string
	TokenHash        string
	Scope            string
	CreatedByUserUID string // empty when the minting admin has since been deleted
	CreatedAt        time.Time
	ExpiresAt        *time.Time // nil = never expires
	LastUsedAt       *time.Time
	RevokedAt        *time.Time // non-nil = revoked, no longer accepted
}

// Active reports whether the token may still authenticate a request: not
// revoked, and either immortal or not yet past its expiry.
func (t *APIToken) Active(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	return t.ExpiresAt == nil || t.ExpiresAt.After(now)
}

// RelationSet selects which optional relations a photo query should expand.
// It is the parsed form of the `?include=labels,albums,markers,files` query
// parameter.
type RelationSet struct {
	Labels  bool
	Albums  bool
	Markers bool
	Files   bool
}

// Empty reports whether no relation at all was requested, letting callers
// skip the expansion queries entirely.
func (r RelationSet) Empty() bool {
	return !r.Labels && !r.Albums && !r.Markers && !r.Files
}
