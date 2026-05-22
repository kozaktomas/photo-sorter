package database

import (
	"context"
	"time"
)

// EmbeddingReader provides read-only access to image embeddings.
type EmbeddingReader interface {
	// Get retrieves an embedding by photo UID, returns nil if not found.
	Get(ctx context.Context, photoUID string) (*StoredEmbedding, error)
	// Has checks if an embedding exists for the given photo UID.
	Has(ctx context.Context, photoUID string) (bool, error)
	// Count returns the total number of embeddings stored.
	Count(ctx context.Context) (int, error)
	// CountByUIDs returns the number of embeddings whose photo_uid is in the given list.
	CountByUIDs(ctx context.Context, uids []string) (int, error)
	// FindSimilar finds the most similar embeddings using cosine distance.
	FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredEmbedding, error)
	// FindSimilarWithDistance finds similar embeddings and returns distances.
	FindSimilarWithDistance(
		ctx context.Context, embedding []float32, limit int, maxDistance float64,
	) ([]StoredEmbedding, []float64, error)
	// GetCentroid returns the AVG(embedding) over the given photo UIDs in
	// one round trip. Returns nil when no rows match. Used by the album
	// suggestion handler to ranks candidates against the centroid of an
	// existing album.
	GetCentroid(ctx context.Context, photoUIDs []string) ([]float32, error)
	// GetUniquePhotoUIDs returns all unique photo UIDs that have embeddings.
	GetUniquePhotoUIDs(ctx context.Context) ([]string, error)
}

// FaceReader provides read-only access to face embeddings.
type FaceReader interface {
	// GetFaces retrieves all faces for a photo.
	GetFaces(ctx context.Context, photoUID string) ([]StoredFace, error)
	// GetFacesBySubjectName retrieves all faces for a specific subject/person by name.
	// This is an optimized query that uses the cached subject_name field.
	// Names are normalized before comparison (lowercase, no diacritics, dashes to spaces).
	// to handle format differences between slugs and display names (e.g., "jan-novak" matches "Jan Novák").
	GetFacesBySubjectName(ctx context.Context, subjectName string) ([]StoredFace, error)
	// HasFaces checks if faces have been computed for a photo.
	HasFaces(ctx context.Context, photoUID string) (bool, error)
	// IsFacesProcessed checks if face detection has been run for a photo (regardless of whether faces were found).
	IsFacesProcessed(ctx context.Context, photoUID string) (bool, error)
	// Count returns the total number of faces stored.
	Count(ctx context.Context) (int, error)
	// CountByUIDs returns the number of faces whose photo_uid is in the given list.
	CountByUIDs(ctx context.Context, uids []string) (int, error)
	// CountPhotos returns the number of distinct photos with faces.
	CountPhotos(ctx context.Context) (int, error)
	// CountPhotosByUIDs returns the number of distinct photos with faces whose photo_uid is in the given list.
	CountPhotosByUIDs(ctx context.Context, uids []string) (int, error)
	// FindSimilar finds faces with similar embeddings using cosine distance.
	FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredFace, error)
	// FindSimilarWithDistance finds similar faces and returns distances.
	FindSimilarWithDistance(
		ctx context.Context, embedding []float32, limit int, maxDistance float64,
	) ([]StoredFace, []float64, error)
	// GetUniquePhotoUIDs returns all unique photo UIDs that have faces.
	GetUniquePhotoUIDs(ctx context.Context) ([]string, error)
	// GetFacesWithMarkerUID returns all faces that have a non-empty marker_uid.
	GetFacesWithMarkerUID(ctx context.Context) ([]StoredFace, error)
	// GetPhotoUIDsWithSubjectName returns a set of photo UIDs (from the
	// given list) that have at least one face assigned to the given
	// subject name. Used to detect photos where a person is already
	// assigned without re-running the marker IoU heuristic.
	GetPhotoUIDsWithSubjectName(ctx context.Context, photoUIDs []string, subjectName string) (map[string]bool, error)
}

// FaceWriter provides write access to face data.
type FaceWriter interface {
	FaceReader

	// SaveFaces stores multiple faces for a photo (replaces existing faces for that photo).
	SaveFaces(ctx context.Context, photoUID string, faces []StoredFace) error

	// MarkFacesProcessed marks a photo as having been processed for face detection.
	MarkFacesProcessed(ctx context.Context, photoUID string, faceCount int) error

	// UpdateFaceMarker updates the cached marker data for a specific face.
	// Used to keep cache in sync when faces are assigned/unassigned via the UI.
	UpdateFaceMarker(ctx context.Context, photoUID string, faceIndex int, markerUID, subjectUID, subjectName string) error

	// UpdateFacePhotoInfo updates the cached photo dimensions and file info for all faces of a photo.
	// Used during processing or backfill to populate cached PhotoPrism data.
	UpdateFacePhotoInfo(ctx context.Context, photoUID string, width, height, orientation int, fileUID string) error

	// DeleteFacesByPhoto removes all faces and faces_processed records for a
	// photo. Returns the deleted face IDs purely for logging/auditing —
	// pgvector keeps the embedding index in sync automatically.
	DeleteFacesByPhoto(ctx context.Context, photoUID string) ([]int64, error)
}

// EmbeddingWriter provides write access to image embeddings.
type EmbeddingWriter interface {
	EmbeddingReader

	// DeleteEmbedding removes the embedding for a photo.
	DeleteEmbedding(ctx context.Context, photoUID string) error
}

// EraEmbeddingReader provides read-only access to era embedding centroids.
type EraEmbeddingReader interface {
	// GetEra retrieves an era embedding by slug, returns nil if not found.
	GetEra(ctx context.Context, eraSlug string) (*StoredEraEmbedding, error)
	// GetAllEras retrieves all era embeddings.
	GetAllEras(ctx context.Context) ([]StoredEraEmbedding, error)
	// CountEras returns the total number of era embeddings stored.
	CountEras(ctx context.Context) (int, error)
}

// EraEmbeddingWriter provides write access to era embedding centroids.
type EraEmbeddingWriter interface {
	EraEmbeddingReader
	// SaveEra stores an era embedding centroid (upsert).
	SaveEra(ctx context.Context, era StoredEraEmbedding) error
	// DeleteEra removes an era embedding by slug.
	DeleteEra(ctx context.Context, eraSlug string) error
}

// BookReader provides read-only access to photo book data.
type BookReader interface {
	GetBook(ctx context.Context, id string) (*PhotoBook, error)
	ListBooks(ctx context.Context) ([]PhotoBook, error)
	ListBooksWithCounts(ctx context.Context) ([]PhotoBookWithCounts, error)
	GetChapter(ctx context.Context, id string) (*BookChapter, error)
	GetChapters(ctx context.Context, bookID string) ([]BookChapter, error)
	GetSection(ctx context.Context, id string) (*BookSection, error)
	GetSections(ctx context.Context, bookID string) ([]BookSection, error)
	GetSectionPhotos(ctx context.Context, sectionID string) ([]SectionPhoto, error)
	CountSectionPhotos(ctx context.Context, sectionID string) (int, error)
	GetPages(ctx context.Context, bookID string) ([]BookPage, error)
	GetPage(ctx context.Context, pageID string) (*BookPage, error)
	GetPageSlots(ctx context.Context, pageID string) ([]PageSlot, error)
	GetPhotoBookMemberships(ctx context.Context, photoUID string) ([]PhotoBookMembership, error)
}

// BookWriter provides write access to photo book data.
type BookWriter interface {
	BookReader
	CreateBook(ctx context.Context, book *PhotoBook) error
	UpdateBook(ctx context.Context, book *PhotoBook) error
	DeleteBook(ctx context.Context, id string) error
	CreateChapter(ctx context.Context, chapter *BookChapter) error
	UpdateChapter(ctx context.Context, chapter *BookChapter) error
	DeleteChapter(ctx context.Context, id string) error
	ReorderChapters(ctx context.Context, bookID string, chapterIDs []string) error
	CreateSection(ctx context.Context, section *BookSection) error
	UpdateSection(ctx context.Context, section *BookSection) error
	DeleteSection(ctx context.Context, id string) error
	ReorderSections(ctx context.Context, bookID string, sectionIDs []string) error
	AddSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error
	RemoveSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error
	UpdateSectionPhoto(ctx context.Context, sectionID string, photoUID string, description string, note string) error
	CreatePage(ctx context.Context, page *BookPage) error
	UpdatePage(ctx context.Context, page *BookPage) error
	DeletePage(ctx context.Context, id string) error
	ReorderPages(ctx context.Context, bookID string, pageIDs []string) error
	// MovePageToSection atomically moves a page to a different section in the
	// same book, appending it at the end of the target section's page order
	// and reconciling the source/target section photo pools for any photos
	// used in the page's slots. Returns ErrPageNotFound if the page does not
	// exist, ErrSectionNotFound if the target section does not exist, and
	// ErrSectionBookMismatch if the target section belongs to a different
	// book.
	MovePageToSection(ctx context.Context, pageID, targetSectionID string) error
	AssignSlot(ctx context.Context, pageID string, slotIndex int, photoUID string) error
	AssignTextSlot(ctx context.Context, pageID string, slotIndex int, textContent string) error
	AssignCaptionsSlot(ctx context.Context, pageID string, slotIndex int) error
	AssignContentsSlot(ctx context.Context, pageID string, slotIndex int) error
	ClearSlot(ctx context.Context, pageID string, slotIndex int) error
	SwapSlots(ctx context.Context, pageID string, slotA int, slotB int) error
	UpdateSlotCrop(ctx context.Context, pageID string, slotIndex int, cropX, cropY, cropScale float64) error
}

// TextVersionStore provides access to text version history.
type TextVersionStore interface {
	SaveTextVersion(ctx context.Context, version *TextVersion) error
	ListTextVersions(ctx context.Context, sourceType, sourceID, field string, limit int) ([]TextVersion, error)
	GetTextVersion(ctx context.Context, id int) (*TextVersion, error)
}

// TextCheckStore provides access to text check results.
type TextCheckStore interface {
	// SaveTextCheckResult upserts a text check result (by source_type, source_id, field).
	SaveTextCheckResult(ctx context.Context, result *TextCheckResult) error
	// GetTextCheckResults returns all check results for the given book's texts.
	// The caller provides (sourceType, sourceID, field) tuples and gets back
	// results keyed by "sourceType:sourceID:field".
	GetTextCheckResults(ctx context.Context, keys []TextCheckKey) (map[string]TextCheckResult, error)
}

// TextCheckKey identifies a specific text field for check result lookup.
type TextCheckKey struct {
	SourceType string
	SourceID   string
	Field      string
}

// AlbumReader provides read-only access to native albums and their
// photo membership rows. Single-row lookups return ErrNotFound when the
// record is missing.
type AlbumReader interface {
	GetAlbum(ctx context.Context, uid string) (*Album, error)
	GetAlbumBySlug(ctx context.Context, slug string) (*Album, error)
	ListAlbums(ctx context.Context, q AlbumQuery) ([]Album, error)
	ListAlbumPhotoUIDs(ctx context.Context, albumUID string) ([]string, error)
	ListAlbumsForPhoto(ctx context.Context, photoUID string) ([]Album, error)
}

// AlbumWriter provides write access to native albums. CreateAlbum generates
// the UID and slug if either is empty; UpdateAlbum overwrites all editable
// columns of the row identified by Album.UID.
type AlbumWriter interface {
	AlbumReader

	CreateAlbum(ctx context.Context, a *Album) error
	UpdateAlbum(ctx context.Context, a *Album) error
	DeleteAlbum(ctx context.Context, uid string) error
	AddPhotos(ctx context.Context, albumUID string, photoUIDs []string) error
	RemovePhotos(ctx context.Context, albumUID string, photoUIDs []string) error
	SetCoverPhoto(ctx context.Context, albumUID, photoUID string) error
}

// PhotoReader provides read-only access to native photos and their physical
// files. Single-row lookups return ErrNotFound when the record is missing.
type PhotoReader interface {
	GetPhoto(ctx context.Context, uid string) (*Photo, error)
	GetPhotoByHash(ctx context.Context, hash string) (*Photo, error)
	ListPhotos(ctx context.Context, filter PhotoFilter) ([]Photo, int, error)
	ListPhotoFiles(ctx context.Context, photoUID string) ([]PhotoFile, error)
	// ListArchivedBefore returns UIDs of photos whose archived_at is strictly
	// before the given cutoff. Used by the trash auto-purge daemon to find
	// photos that have outlived the retention window.
	ListArchivedBefore(ctx context.Context, cutoff time.Time) ([]string, error)
}

// PhotoBrowseReader provides read-only access to the aggregate views that
// power the Browse page: a date histogram and a list of (uid, lat, lng)
// tuples for every photo with non-NULL coordinates. Both honour the full
// PhotoFilter contract (geo bbox, label/subject/favorite/q, taken-range),
// minus pagination which the histogram/geo endpoints do not expose.
//
// Kept separate from PhotoReader so the existing in-memory test mocks for
// ListPhotos / GetPhoto do not need to grow to satisfy these new methods.
type PhotoBrowseReader interface {
	// Histogram returns the photo-count distribution across time. bucket
	// must be either "month" or "year"; any other value returns an error.
	Histogram(ctx context.Context, filter PhotoFilter, bucket string) (HistogramResult, error)

	// ListGeoPoints returns one row per photo that matches the filter and
	// has non-NULL lat/lng. The result is capped at maxPoints (0 disables
	// the cap); the second return is true when the cap kicked in so the
	// caller can surface a "truncated" warning to the UI.
	ListGeoPoints(ctx context.Context, filter PhotoFilter, maxPoints int) ([]GeoPoint, bool, error)
}

// PhotoWriter provides write access to native photos and their physical
// files. Archive/Restore toggle the archived_at column; DeletePhoto is a
// hard delete that cascades to photo_files.
type PhotoWriter interface {
	PhotoReader
	CreatePhoto(ctx context.Context, p *Photo) error
	UpdatePhoto(ctx context.Context, p *Photo) error
	DeletePhoto(ctx context.Context, uid string) error
	ArchivePhoto(ctx context.Context, uid string) error
	RestorePhoto(ctx context.Context, uid string) error
	AddPhotoFile(ctx context.Context, f *PhotoFile) error
	DeletePhotoFile(ctx context.Context, photoUID, filePath string) error
}

// PHashReader provides read-only access to the photo_phashes table. It is
// used by the upload pipeline's near-duplicate detector to find photos whose
// pHash differs from a candidate by a small Hamming distance.
type PHashReader interface {
	// GetPHash returns the stored pHash + dHash for the given photo. Returns
	// ErrNotFound when no row exists yet (the photo has not been backfilled).
	GetPHash(ctx context.Context, photoUID string) (*PhotoPHash, error)
	// ListAllPHashes returns every row in photo_phashes. The duplicate detector
	// scans the full set in memory and computes hamming distance to the
	// candidate — pHash is 8 bytes so a million rows fits in ~8 MB.
	ListAllPHashes(ctx context.Context) ([]PhotoPHash, error)
	// CountPHashes returns the number of rows in photo_phashes. Used by the
	// backfill CLI to size progress bars.
	CountPHashes(ctx context.Context) (int, error)
	// ListPhotosWithoutPHash returns photo UIDs that have no row in
	// photo_phashes yet, up to limit rows (0 = no limit). Drives the
	// `cache compute-phashes` backfill.
	ListPhotosWithoutPHash(ctx context.Context, limit int) ([]string, error)
}

// PHashWriter provides write access to the photo_phashes table. Save is an
// upsert keyed by photo_uid so the upload pipeline and the backfill CLI can
// safely race without producing duplicate rows.
type PHashWriter interface {
	PHashReader

	// SavePHash upserts the pHash + dHash for a photo.
	SavePHash(ctx context.Context, photoUID string, phash, dhash uint64) error
	// DeletePHash removes a photo's pHash row. Mostly used by tests; in
	// production rows cascade away when the photo is hard-deleted.
	DeletePHash(ctx context.Context, photoUID string) error
}

// LabelReader provides read-only access to native labels and the
// photo_labels junction. Single-row lookups return ErrNotFound when the
// requested record is missing.
type LabelReader interface {
	GetLabel(ctx context.Context, uid string) (*Label, error)
	GetLabelBySlug(ctx context.Context, slug string) (*Label, error)
	ListLabels(ctx context.Context, q LabelQuery) ([]Label, error)
	ListLabelsForPhoto(ctx context.Context, photoUID string) ([]Label, error)
}

// LabelWriter provides write access to native labels and the photo_labels
// junction. EnsureLabel upserts a label by slug so concurrent callers in the
// AI sort pipeline cannot race-create duplicate rows; AddPhotoLabel is
// idempotent via the (photo_uid, label_uid) primary key. DeleteLabels
// returns the number of rows that were actually deleted, so a request
// containing mixed valid and invalid UIDs surfaces the real count.
type LabelWriter interface {
	LabelReader

	EnsureLabel(ctx context.Context, name string) (*Label, error)
	UpdateLabel(ctx context.Context, l *Label) error
	DeleteLabels(ctx context.Context, uids []string) (int, error)
	AddPhotoLabel(ctx context.Context, photoUID, labelUID, source string, uncertainty int) error
	RemovePhotoLabel(ctx context.Context, photoUID, labelUID string) error
}

// UserReader provides read-only access to the native user store.
// GetUser/GetUserByUsername return ErrNotFound when the row is missing.
// GetUserByUsername returns the bcrypt password hash so the login flow can
// verify a credential attempt; callers must not propagate the returned
// struct beyond the verification step.
type UserReader interface {
	GetUser(ctx context.Context, uid string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*UserWithSecret, error)
	ListUsers(ctx context.Context) ([]User, error)
	CountUsers(ctx context.Context) (int, error)
}

// MarkerReader provides read-only access to face/label markers.
// Single-row lookups return ErrNotFound when the row does not exist.
type MarkerReader interface {
	GetMarker(ctx context.Context, uid string) (*Marker, error)
	ListMarkersForPhoto(ctx context.Context, photoUID string) ([]Marker, error)
	ListMarkersForSubject(ctx context.Context, subjectUID string, limit, offset int) ([]Marker, error)
}

// MarkerWriter provides write access to face/label markers. CreateMarker
// generates m.UID when empty. AssignSubject/UnassignSubject toggle the
// subject_uid column; SetInvalid toggles the invalid flag (which excludes
// the marker from subject photo/face counts).
type MarkerWriter interface {
	MarkerReader

	CreateMarker(ctx context.Context, m *Marker) error
	UpdateMarker(ctx context.Context, m *Marker) error
	DeleteMarker(ctx context.Context, uid string) error
	AssignSubject(ctx context.Context, markerUID, subjectUID string) error
	UnassignSubject(ctx context.Context, markerUID string) error
	SetInvalid(ctx context.Context, markerUID string, invalid bool) error
}

// SubjectReader provides read-only access to subjects (people / pets /
// other recurring targets). Single-row lookups return ErrNotFound when
// the row does not exist. GetSubjectByName uses the unaccent extension
// and a lowercase comparison, so accent/case variants map to the same row.
type SubjectReader interface {
	GetSubject(ctx context.Context, uid string) (*Subject, error)
	GetSubjectByName(ctx context.Context, name string) (*Subject, error)
	ListSubjects(ctx context.Context, q SubjectQuery) ([]Subject, error)
	ListSubjectsForPhoto(ctx context.Context, photoUID string) ([]Subject, error)
}

// SubjectWriter provides write access to subjects. EnsureSubject upserts
// by accent-insensitive lowercased name so concurrent callers cannot
// race-create duplicate rows; the slug is generated from the name.
// EnsureSubjectWithUID behaves the same but uses the supplied UID
// verbatim on insert — the PhotoPrism migrator uses it to preserve
// subj_uid so cached references in faces.subject_uid keep working.
type SubjectWriter interface {
	SubjectReader

	EnsureSubject(ctx context.Context, name, subjectType string) (*Subject, error)
	EnsureSubjectWithUID(ctx context.Context, uid, name, subjectType string) (*Subject, error)
	UpdateSubject(ctx context.Context, s *Subject) error
	DeleteSubject(ctx context.Context, uid string) error
}

// ShareLinkReader provides read-only access to album share links.
type ShareLinkReader interface {
	// GetShareLink returns the share link identified by slug. Returns
	// ErrNotFound when no row exists.
	GetShareLink(ctx context.Context, slug string) (*ShareLink, error)
	// ListShareLinksForAlbum returns every share link pointing at the
	// given album, ordered by created_at DESC. Used by the auth-side
	// album detail UI.
	ListShareLinksForAlbum(ctx context.Context, albumUID string) ([]ShareLink, error)
}

// ShareLinkWriter provides write access to album share links. CreateShareLink
// inserts a new row and returns ErrShareLinkSlugTaken on a primary-key
// collision. DeleteShareLink removes the row identified by slug and returns
// ErrNotFound when nothing was deleted.
type ShareLinkWriter interface {
	ShareLinkReader

	CreateShareLink(ctx context.Context, link *ShareLink) error
	DeleteShareLink(ctx context.Context, slug string) error
}

// SmartAlbumReader provides read-only access to saved photo searches
// ("smart albums"). The actual photo evaluation against a smart album's
// filters runs through PhotoReader.ListPhotos — these methods only manage
// the stored definition, never the resolved photo list.
type SmartAlbumReader interface {
	// GetSmartAlbum returns the smart album identified by uid. Returns
	// ErrNotFound when no row exists.
	GetSmartAlbum(ctx context.Context, uid string) (*SmartAlbum, error)
	// ListSmartAlbums returns every smart album, ordered by created_at DESC.
	// We intentionally do not paginate: smart albums are a per-user concept
	// with a small cardinality (manual creations).
	ListSmartAlbums(ctx context.Context) ([]SmartAlbum, error)
}

// SmartAlbumWriter extends SmartAlbumReader with the mutating endpoints
// needed by `POST /api/v1/smart-albums`, the update flow, and delete.
// CreateSmartAlbum generates album.UID via the postgres helper when it is
// empty.
type SmartAlbumWriter interface {
	SmartAlbumReader

	CreateSmartAlbum(ctx context.Context, album *SmartAlbum) error
	UpdateSmartAlbum(ctx context.Context, album *SmartAlbum) error
	DeleteSmartAlbum(ctx context.Context, uid string) error
}

// PhotoEditsReader provides read-only access to the photo_edits table.
// GetPhotoEdits returns ErrNotFound when no row exists for the photo (i.e.
// no non-destructive edits have been applied).
type PhotoEditsReader interface {
	GetPhotoEdits(ctx context.Context, photoUID string) (*PhotoEdits, error)
}

// PhotoEditsWriter provides write access to the photo_edits table. Save
// is an upsert keyed by photo_uid so concurrent edits from the UI cannot
// produce duplicate rows.  Delete returns nil even when no row was
// present (idempotent revert-to-original).
type PhotoEditsWriter interface {
	PhotoEditsReader

	SavePhotoEdits(ctx context.Context, edits *PhotoEdits) error
	DeletePhotoEdits(ctx context.Context, photoUID string) error
}

// AuditLogReader provides read-only access to the audit_log table. The
// audit log is append-only; there are no single-row lookups by UID.
type AuditLogReader interface {
	// ListAuditLog returns audit log entries matching the filter, ordered
	// by created_at DESC. The second return value is the total row count
	// matching the filter (ignoring Limit/Offset) so the UI can render
	// pagination without a second round trip.
	ListAuditLog(ctx context.Context, filter AuditLogFilter) ([]AuditLogEntry, int, error)
}

// AuditLogWriter provides append-only write access to the audit_log table.
// There is intentionally no Update or Delete; the trail is forever.
type AuditLogWriter interface {
	AuditLogReader
	// AppendAuditLog inserts a single audit log row. Failures must never
	// fail the caller's underlying request: the upstream audit.Logger
	// swallows + WARN-logs errors and returns nil up the chain.
	AppendAuditLog(ctx context.Context, entry *AuditLogEntry) error
}

// UserWriter provides write access to the native user store. CreateUser
// generates u.UID when empty. Username uniqueness is enforced by the
// underlying UNIQUE index and surfaced as ErrUsernameTaken; all other
// single-row writes return ErrNotFound when the target user does not exist.
type UserWriter interface {
	UserReader

	CreateUser(ctx context.Context, u *UserWithSecret) error
	UpdateUser(ctx context.Context, u *User) error
	SetPassword(ctx context.Context, uid, newHash string) error
	SetDisabled(ctx context.Context, uid string, disabled bool) error
	TouchLastLogin(ctx context.Context, uid string) error
	DeleteUser(ctx context.Context, uid string) error
}
