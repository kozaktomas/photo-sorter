// Package postgres provides PostgreSQL-backed repository implementations.
package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/lib/pq"
)

const (
	// defaultPhotoListLimit is the default page size for ListPhotos when
	// PhotoFilter.Limit is 0.
	defaultPhotoListLimit = 50

	// maxPhotoListLimit caps PhotoFilter.Limit; values larger than this are
	// clamped down silently.
	maxPhotoListLimit = 500

	// photoUIDRandLen is the number of random base32 characters appended
	// after the "p" prefix in a generated photo UID.
	photoUIDRandLen = 16

	// photoUIDPrefix is the single-character type prefix for photo UIDs.
	photoUIDPrefix = "p"
)

// photoColumns is the canonical column list for SELECT statements against
// the photos table. The order here matches scanPhoto below.
const photoColumns = `uid, file_hash, file_path, file_name, file_size, file_mime,
	file_width, file_height, file_orientation,
	taken_at, taken_at_source, title, description, notes,
	lat, lng, altitude,
	camera_make, camera_model, lens_model,
	iso, aperture, exposure, focal_length, exif,
	favorite, private, archived_at, uploaded_by, created_at, updated_at`

// photoFileColumns is the canonical column list for SELECT statements
// against the photo_files table.
const photoFileColumns = `id, photo_uid, file_path, file_hash, file_size, file_mime,
	is_primary, role, created_at`

// PhotoRepository provides PostgreSQL-backed storage for photos and their
// physical files. It implements database.PhotoReader and database.PhotoWriter.
type PhotoRepository struct {
	pool *Pool
}

// NewPhotoRepository returns a PhotoRepository bound to the given pool.
func NewPhotoRepository(pool *Pool) *PhotoRepository {
	return &PhotoRepository{pool: pool}
}

// NewPhotoUID returns a freshly generated photo UID. The format is
// `"p" + 16 lowercase base32 characters`, e.g. "pa3bz5x9k8mq7n2v".
// The random suffix is drawn from crypto/rand; the function panics if the
// system random source fails, since callers cannot meaningfully recover.
func NewPhotoUID() string {
	// Base32 packs 5 bits per character, so photoUIDRandLen chars need
	// ceil(photoUIDRandLen*5/8) random bytes; we then truncate the encoded
	// output to the desired length.
	randBytes := (photoUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("photo uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return photoUIDPrefix + strings.ToLower(enc[:photoUIDRandLen])
}

// --- Reads ---

// GetPhoto fetches a single photo by UID. Returns database.ErrNotFound when
// the row does not exist.
func (r *PhotoRepository) GetPhoto(ctx context.Context, uid string) (*database.Photo, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+photoColumns+` FROM photos WHERE uid = $1`, uid)
	p, err := scanPhoto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get photo: %w", err)
	}
	return p, nil
}

// GetPhotoByHash fetches a photo by its file_hash. Returns
// database.ErrNotFound when no such row exists. The hash column is unique,
// so at most one row can match.
func (r *PhotoRepository) GetPhotoByHash(ctx context.Context, hash string) (*database.Photo, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+photoColumns+` FROM photos WHERE file_hash = $1`, hash)
	p, err := scanPhoto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get photo by hash: %w", err)
	}
	return p, nil
}

// ListPhotos returns a page of photos matching the given filter, along with
// the total count of matching rows (independent of pagination).
func (r *PhotoRepository) ListPhotos(
	ctx context.Context, filter database.PhotoFilter,
) ([]database.Photo, int, error) {
	where, args := buildPhotoFilter(filter)
	orderBy := photoOrderBy(filter.SortBy)
	limit, offset := paginationBounds(filter)

	countSQL := "SELECT COUNT(*) FROM photos p" + photoFilterJoins(filter) + where
	var total int
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count photos: %w", err)
	}

	listSQL := "SELECT " + qualifyPhotoColumns("p") + " FROM photos p" +
		photoFilterJoins(filter) + where +
		" ORDER BY " + orderBy +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list photos: %w", err)
	}
	defer rows.Close()

	var photos []database.Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan photo: %w", err)
		}
		photos = append(photos, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate photos: %w", err)
	}
	return photos, total, nil
}

// ListPhotoFiles returns every photo_files row belonging to the given photo,
// ordered with the primary file first then by ID for stability.
func (r *PhotoRepository) ListPhotoFiles(
	ctx context.Context, photoUID string,
) ([]database.PhotoFile, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+photoFileColumns+` FROM photo_files
		 WHERE photo_uid = $1
		 ORDER BY is_primary DESC, id`, photoUID)
	if err != nil {
		return nil, fmt.Errorf("list photo files: %w", err)
	}
	defer rows.Close()
	var files []database.PhotoFile
	for rows.Next() {
		var f database.PhotoFile
		if err := rows.Scan(
			&f.ID, &f.PhotoUID, &f.FilePath, &f.FileHash, &f.FileSize, &f.FileMime,
			&f.IsPrimary, &f.Role, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan photo file: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photo files: %w", err)
	}
	return files, nil
}

// ListArchivedBefore returns UIDs of photos whose archived_at is strictly
// before cutoff. The result is ordered by archived_at ascending so a caller
// purging in chunks sees the oldest rows first.
func (r *PhotoRepository) ListArchivedBefore(
	ctx context.Context, cutoff time.Time,
) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT uid FROM photos
		 WHERE archived_at IS NOT NULL AND archived_at < $1
		 ORDER BY archived_at ASC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list archived before: %w", err)
	}
	defer rows.Close()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan archived uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archived uids: %w", err)
	}
	return uids, nil
}

// --- Writes ---

// CreatePhoto inserts a new photo, generating a UID via NewPhotoUID when
// p.UID is empty. The created/updated timestamps are set to NOW() by the
// database and copied back into p on success.
func (r *PhotoRepository) CreatePhoto(ctx context.Context, p *database.Photo) error {
	if p.UID == "" {
		p.UID = NewPhotoUID()
	}
	if p.TakenAtSource == "" {
		p.TakenAtSource = "unknown"
	}
	exifJSON, err := marshalExif(p.Exif)
	if err != nil {
		return fmt.Errorf("create photo: marshal exif: %w", err)
	}
	uploadedBy := nullableString(p.UploadedBy)
	row := r.pool.QueryRow(ctx,
		`INSERT INTO photos (
			uid, file_hash, file_path, file_name, file_size, file_mime,
			file_width, file_height, file_orientation,
			taken_at, taken_at_source, title, description, notes,
			lat, lng, altitude,
			camera_make, camera_model, lens_model,
			iso, aperture, exposure, focal_length, exif,
			favorite, private, archived_at, uploaded_by
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9,
			$10, $11, $12, $13, $14,
			$15, $16, $17,
			$18, $19, $20,
			$21, $22, $23, $24, $25::jsonb,
			$26, $27, $28, $29
		)
		RETURNING created_at, updated_at`,
		p.UID, p.FileHash, p.FilePath, p.FileName, p.FileSize, p.FileMime,
		p.FileWidth, p.FileHeight, p.FileOrientation,
		p.TakenAt, p.TakenAtSource, p.Title, p.Description, p.Notes,
		p.Lat, p.Lng, p.Altitude,
		p.CameraMake, p.CameraModel, p.LensModel,
		p.ISO, p.Aperture, p.Exposure, p.FocalLength, exifJSON,
		p.Favorite, p.Private, p.ArchivedAt, uploadedBy,
	)
	if err := row.Scan(&p.CreatedAt, &p.UpdatedAt); err != nil {
		return fmt.Errorf("create photo: %w", err)
	}
	return nil
}

// UpdatePhoto writes the supplied photo back to the database. All editable
// columns are overwritten; created_at is not modified. updated_at is bumped
// to NOW() by the statement and copied back into p.
func (r *PhotoRepository) UpdatePhoto(ctx context.Context, p *database.Photo) error {
	exifJSON, err := marshalExif(p.Exif)
	if err != nil {
		return fmt.Errorf("update photo: marshal exif: %w", err)
	}
	uploadedBy := nullableString(p.UploadedBy)
	row := r.pool.QueryRow(ctx,
		`UPDATE photos SET
			file_hash = $1, file_path = $2, file_name = $3,
			file_size = $4, file_mime = $5,
			file_width = $6, file_height = $7, file_orientation = $8,
			taken_at = $9, taken_at_source = $10,
			title = $11, description = $12, notes = $13,
			lat = $14, lng = $15, altitude = $16,
			camera_make = $17, camera_model = $18, lens_model = $19,
			iso = $20, aperture = $21, exposure = $22, focal_length = $23,
			exif = $24::jsonb,
			favorite = $25, private = $26, archived_at = $27,
			uploaded_by = $28,
			updated_at = NOW()
		 WHERE uid = $29
		 RETURNING updated_at`,
		p.FileHash, p.FilePath, p.FileName,
		p.FileSize, p.FileMime,
		p.FileWidth, p.FileHeight, p.FileOrientation,
		p.TakenAt, p.TakenAtSource,
		p.Title, p.Description, p.Notes,
		p.Lat, p.Lng, p.Altitude,
		p.CameraMake, p.CameraModel, p.LensModel,
		p.ISO, p.Aperture, p.Exposure, p.FocalLength,
		exifJSON,
		p.Favorite, p.Private, p.ArchivedAt,
		uploadedBy,
		p.UID,
	)
	if err := row.Scan(&p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("update photo: %w", err)
	}
	return nil
}

// DeletePhoto hard-deletes the photo row; photo_files rows cascade via the
// FK ON DELETE CASCADE clause. Returns database.ErrNotFound when no row is
// affected.
func (r *PhotoRepository) DeletePhoto(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx, `DELETE FROM photos WHERE uid = $1`, uid)
	if err != nil {
		return fmt.Errorf("delete photo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete photo rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ArchivePhoto sets archived_at = NOW() for the given photo (soft delete).
// Returns database.ErrNotFound when no row is affected.
func (r *PhotoRepository) ArchivePhoto(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE photos SET archived_at = NOW(), updated_at = NOW()
		 WHERE uid = $1 AND archived_at IS NULL`, uid)
	if err != nil {
		return fmt.Errorf("archive photo: %w", err)
	}
	if err := ensureRowAffectedOrExists(ctx, r, uid, res); err != nil {
		return err
	}
	return nil
}

// RestorePhoto clears archived_at for the given photo. Returns
// database.ErrNotFound when no row is affected.
func (r *PhotoRepository) RestorePhoto(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE photos SET archived_at = NULL, updated_at = NOW()
		 WHERE uid = $1 AND archived_at IS NOT NULL`, uid)
	if err != nil {
		return fmt.Errorf("restore photo: %w", err)
	}
	if err := ensureRowAffectedOrExists(ctx, r, uid, res); err != nil {
		return err
	}
	return nil
}

// AddPhotoFile inserts a photo_files row for the given file. The caller
// supplies the photo UID and the metadata; the row's ID and created_at are
// populated on the struct on success.
func (r *PhotoRepository) AddPhotoFile(ctx context.Context, f *database.PhotoFile) error {
	if f.Role == "" {
		f.Role = "original"
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO photo_files (
			photo_uid, file_path, file_hash, file_size, file_mime,
			is_primary, role
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		f.PhotoUID, f.FilePath, f.FileHash, f.FileSize, f.FileMime,
		f.IsPrimary, f.Role,
	)
	if err := row.Scan(&f.ID, &f.CreatedAt); err != nil {
		return fmt.Errorf("add photo file: %w", err)
	}
	return nil
}

// DeletePhotoFile removes the photo_files row identified by (photo_uid,
// file_path). Returns database.ErrNotFound when no row matches.
func (r *PhotoRepository) DeletePhotoFile(
	ctx context.Context, photoUID, filePath string,
) error {
	res, err := r.pool.Exec(ctx,
		`DELETE FROM photo_files WHERE photo_uid = $1 AND file_path = $2`,
		photoUID, filePath)
	if err != nil {
		return fmt.Errorf("delete photo file: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete photo file rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// --- Helpers ---

// scanPhoto reads a single photos row from any *sql.Row or *sql.Rows.
func scanPhoto(s rowScanner) (*database.Photo, error) {
	var (
		p          database.Photo
		exifBytes  []byte
		uploadedBy sql.NullString
	)
	err := s.Scan(
		&p.UID, &p.FileHash, &p.FilePath, &p.FileName, &p.FileSize, &p.FileMime,
		&p.FileWidth, &p.FileHeight, &p.FileOrientation,
		&p.TakenAt, &p.TakenAtSource, &p.Title, &p.Description, &p.Notes,
		&p.Lat, &p.Lng, &p.Altitude,
		&p.CameraMake, &p.CameraModel, &p.LensModel,
		&p.ISO, &p.Aperture, &p.Exposure, &p.FocalLength, &exifBytes,
		&p.Favorite, &p.Private, &p.ArchivedAt, &uploadedBy,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan photo row: %w", err)
	}
	p.UploadedBy = uploadedBy.String
	p.Exif, err = unmarshalExif(exifBytes)
	if err != nil {
		return nil, fmt.Errorf("unmarshal exif: %w", err)
	}
	return &p, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows so scanPhoto can
// be shared between single-row and multi-row callers.
type rowScanner interface {
	Scan(dest ...any) error
}

// qualifyPhotoColumns prefixes every comma-separated column in photoColumns
// with the given table alias (e.g. "p.uid, p.file_hash, ..."). Used when a
// JOIN forces the SELECT list to qualify ambiguous names.
func qualifyPhotoColumns(alias string) string {
	parts := strings.Split(photoColumns, ",")
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		col := strings.TrimSpace(raw)
		out = append(out, alias+"."+col)
	}
	return strings.Join(out, ", ")
}

// photoFilterJoins returns the SQL JOIN fragment needed to evaluate the
// label/subject/album filters. Photos are joined to album_photos,
// photo_labels and markers via correlated EXISTS subqueries embedded in the
// WHERE clause, so no joins are needed; this is kept as a hook for future
// extension and currently returns an empty string.
func photoFilterJoins(filter database.PhotoFilter) string {
	_ = filter
	return ""
}

// buildPhotoFilter constructs the WHERE clause and the matching argument
// slice for ListPhotos. It returns (clause, args) where clause is either
// empty or begins with " WHERE ".
func buildPhotoFilter(filter database.PhotoFilter) (string, []any) {
	b := newPhotoFilterBuilder()
	b.applyArchived(filter.Archived)
	b.applyAlbum(filter.AlbumUID)
	b.applyLabels(filter.LabelUIDs)
	b.applySubjects(filter.SubjectUIDs)
	b.applyFavorite(filter.Favorite)
	b.applyPrivate(filter.Private)
	b.applyTakenRange(filter.TakenFrom, filter.TakenTo)
	b.applyBBox(filter.BBox)
	b.applyUploadedBy(filter.UploadedBy)
	b.applySearch(filter.Search)
	return b.build()
}

// photoFilterBuilder accumulates WHERE clauses and bind args while keeping
// placeholder numbering in sync.
type photoFilterBuilder struct {
	clauses []string
	args    []any
}

func newPhotoFilterBuilder() *photoFilterBuilder {
	return &photoFilterBuilder{}
}

// next returns the next "$N" placeholder and registers the argument.
func (b *photoFilterBuilder) next(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

// add appends a clause expressed in terms of the already-registered args.
func (b *photoFilterBuilder) add(clause string) {
	b.clauses = append(b.clauses, clause)
}

func (b *photoFilterBuilder) applyArchived(archived *bool) {
	switch {
	case archived == nil:
		b.add("p.archived_at IS NULL")
	case *archived:
		b.add("p.archived_at IS NOT NULL")
	default:
		b.add("p.archived_at IS NULL")
	}
}

func (b *photoFilterBuilder) applyAlbum(albumUID string) {
	if albumUID == "" {
		return
	}
	ph := b.next(albumUID)
	b.add(fmt.Sprintf(
		"EXISTS (SELECT 1 FROM album_photos ap WHERE ap.photo_uid = p.uid AND ap.album_uid = %s)",
		ph,
	))
}

func (b *photoFilterBuilder) applyLabels(labelUIDs []string) {
	for _, uid := range labelUIDs {
		ph := b.next(uid)
		b.add(fmt.Sprintf(
			"EXISTS (SELECT 1 FROM photo_labels pl WHERE pl.photo_uid = p.uid AND pl.label_uid = %s)",
			ph,
		))
	}
}

func (b *photoFilterBuilder) applySubjects(subjectUIDs []string) {
	for _, uid := range subjectUIDs {
		ph := b.next(uid)
		b.add(fmt.Sprintf(
			"EXISTS (SELECT 1 FROM markers m WHERE m.photo_uid = p.uid AND m.subject_uid = %s)",
			ph,
		))
	}
}

func (b *photoFilterBuilder) applyFavorite(favorite *bool) {
	if favorite == nil {
		return
	}
	ph := b.next(*favorite)
	b.add("p.favorite = " + ph)
}

func (b *photoFilterBuilder) applyPrivate(private *bool) {
	if private == nil {
		return
	}
	ph := b.next(*private)
	b.add("p.private = " + ph)
}

func (b *photoFilterBuilder) applyTakenRange(from, to *time.Time) {
	if from != nil {
		ph := b.next(*from)
		b.add("p.taken_at >= " + ph)
	}
	if to != nil {
		ph := b.next(*to)
		b.add("p.taken_at <= " + ph)
	}
}

func (b *photoFilterBuilder) applyBBox(box *database.BBox) {
	if box == nil {
		return
	}
	minLat := b.next(box.MinLat)
	maxLat := b.next(box.MaxLat)
	minLng := b.next(box.MinLng)
	maxLng := b.next(box.MaxLng)
	b.add(fmt.Sprintf(
		"p.lat BETWEEN %s AND %s AND p.lng BETWEEN %s AND %s",
		minLat, maxLat, minLng, maxLng,
	))
}

func (b *photoFilterBuilder) applyUploadedBy(uid string) {
	if uid == "" {
		return
	}
	ph := b.next(uid)
	b.add("p.uploaded_by = " + ph)
}

func (b *photoFilterBuilder) applySearch(search string) {
	search = strings.TrimSpace(search)
	if search == "" {
		return
	}
	ph := b.next("%" + search + "%")
	b.add(fmt.Sprintf(
		"(p.title ILIKE %s OR p.description ILIKE %s OR p.file_name ILIKE %s)",
		ph, ph, ph,
	))
}

func (b *photoFilterBuilder) build() (string, []any) {
	if len(b.clauses) == 0 {
		return "", b.args
	}
	return " WHERE " + strings.Join(b.clauses, " AND "), b.args
}

// photoOrderBy translates the public sort key into a SQL ORDER BY expression.
// The trailing "uid DESC" makes ordering deterministic when sort keys tie.
func photoOrderBy(sortBy string) string {
	switch sortBy {
	case "oldest":
		return "p.taken_at ASC NULLS LAST, p.uid DESC"
	case "name":
		return "p.file_name ASC, p.uid DESC"
	case "newest", "":
		return "p.taken_at DESC NULLS LAST, p.uid DESC"
	default:
		return "p.taken_at DESC NULLS LAST, p.uid DESC"
	}
}

// paginationBounds clamps the user-supplied limit/offset into safe ranges.
func paginationBounds(filter database.PhotoFilter) (int, int) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPhotoListLimit
	}
	if limit > maxPhotoListLimit {
		limit = maxPhotoListLimit
	}
	offset := max(filter.Offset, 0)
	return limit, offset
}

// marshalExif converts the EXIF map into a JSONB-ready byte slice. A nil
// map becomes the empty object `{}`.
func marshalExif(exif map[string]any) ([]byte, error) {
	if exif == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(exif)
	if err != nil {
		return nil, fmt.Errorf("marshal exif json: %w", err)
	}
	return b, nil
}

// unmarshalExif decodes the JSONB bytes back into a map. Empty or nil input
// returns nil.
func unmarshalExif(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal exif json: %w", err)
	}
	return out, nil
}

// nullableString converts an empty string to a NULL value for INSERT/UPDATE.
// A non-empty string is passed through unchanged.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ensureRowAffectedOrExists distinguishes between "no such photo" (which
// returns database.ErrNotFound) and "photo exists but the state guard in
// the UPDATE prevented the row from changing" (which is a no-op success).
func ensureRowAffectedOrExists(
	ctx context.Context, r *PhotoRepository, uid string, res sql.Result,
) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM photos WHERE uid = $1)`, uid).Scan(&exists); err != nil {
		return fmt.Errorf("check photo exists: %w", err)
	}
	if !exists {
		return database.ErrNotFound
	}
	return nil
}

// IsUniqueViolation reports whether err is a PostgreSQL unique_violation
// (SQLSTATE 23505), regardless of constraint name. Exposed for callers
// (and tests) that need to distinguish duplicate inserts from other errors.
func IsUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	return pqErr.Code == "23505"
}

// Verify interface compliance.
var (
	_ database.PhotoReader = (*PhotoRepository)(nil)
	_ database.PhotoWriter = (*PhotoRepository)(nil)
)
