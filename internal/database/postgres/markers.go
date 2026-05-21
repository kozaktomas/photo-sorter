package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

const (
	// markerUIDPrefix is the single-character type prefix for marker UIDs.
	markerUIDPrefix = "m"

	// markerUIDRandLen is the number of random base32 characters appended
	// after the "m" prefix in a generated marker UID.
	markerUIDRandLen = 16

	// markerTypeDefault is the default value for the markers.type column
	// when the caller does not specify one. It matches the CHECK
	// constraint set in migration 032.
	markerTypeDefault = "face"

	// defaultMarkerListLimit is the default page size for
	// ListMarkersForSubject when the caller passes limit <= 0.
	defaultMarkerListLimit = 500

	// maxMarkerListLimit caps ListMarkersForSubject limit; larger values
	// are clamped down silently.
	maxMarkerListLimit = 5000
)

// markerColumns is the canonical column list for SELECT statements
// against the markers table. The order here matches scanMarker below.
const markerColumns = `uid, photo_uid, COALESCE(subject_uid, ''),
	type, x, y, w, h, score, invalid, reviewed,
	created_at, updated_at`

// MarkerRepository provides PostgreSQL-backed storage for markers.
// It implements database.MarkerReader and database.MarkerWriter.
type MarkerRepository struct {
	pool *Pool
}

// NewMarkerRepository returns a MarkerRepository bound to the given
// pool.
func NewMarkerRepository(pool *Pool) *MarkerRepository {
	return &MarkerRepository{pool: pool}
}

// NewMarkerUID returns a freshly generated marker UID. The format is
// `"m" + 16 lowercase base32 characters`, e.g. "m3z9k8mq7n2v4xpa".
// The random suffix is drawn from crypto/rand; the function panics if
// the system random source fails, since callers cannot meaningfully
// recover.
func NewMarkerUID() string {
	randBytes := (markerUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("marker uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return markerUIDPrefix + strings.ToLower(enc[:markerUIDRandLen])
}

// --- Reads ---

// GetMarker fetches a single marker by UID. Returns database.ErrNotFound
// when the row does not exist.
func (r *MarkerRepository) GetMarker(
	ctx context.Context, uid string,
) (*database.Marker, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+markerColumns+` FROM markers WHERE uid = $1`, uid)
	m, err := scanMarker(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get marker: %w", err)
	}
	return m, nil
}

// ListMarkersForPhoto returns every marker attached to the given photo,
// ordered by created_at then UID for deterministic iteration.
func (r *MarkerRepository) ListMarkersForPhoto(
	ctx context.Context, photoUID string,
) ([]database.Marker, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+markerColumns+` FROM markers
		 WHERE photo_uid = $1
		 ORDER BY created_at ASC, uid ASC`, photoUID)
	if err != nil {
		return nil, fmt.Errorf("list markers for photo: %w", err)
	}
	defer rows.Close()
	var markers []database.Marker
	for rows.Next() {
		m, err := scanMarker(rows)
		if err != nil {
			return nil, fmt.Errorf("scan marker for photo: %w", err)
		}
		markers = append(markers, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate markers for photo: %w", err)
	}
	return markers, nil
}

// ListMarkersForSubject returns a paged slice of markers attached to the
// given subject, ordered by created_at descending then UID. Markers
// flagged invalid are included so the caller can audit false positives;
// downstream filtering by invalid=FALSE is the caller's responsibility.
// limit <= 0 falls back to defaultMarkerListLimit; values above
// maxMarkerListLimit are clamped down silently.
func (r *MarkerRepository) ListMarkersForSubject(
	ctx context.Context, subjectUID string, limit, offset int,
) ([]database.Marker, error) {
	if limit <= 0 {
		limit = defaultMarkerListLimit
	}
	if limit > maxMarkerListLimit {
		limit = maxMarkerListLimit
	}
	offset = max(offset, 0)
	rows, err := r.pool.Query(ctx,
		`SELECT `+markerColumns+` FROM markers
		 WHERE subject_uid = $1
		 ORDER BY created_at DESC, uid ASC
		 LIMIT $2 OFFSET $3`, subjectUID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list markers for subject: %w", err)
	}
	defer rows.Close()
	var markers []database.Marker
	for rows.Next() {
		m, err := scanMarker(rows)
		if err != nil {
			return nil, fmt.Errorf("scan marker for subject: %w", err)
		}
		markers = append(markers, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate markers for subject: %w", err)
	}
	return markers, nil
}

// --- Writes ---

// CreateMarker inserts a new marker, generating a UID via NewMarkerUID
// when m.UID is empty. Defaults are applied for the type column. The
// created/updated timestamps are set to NOW() by the database and copied
// back into m on success.
func (r *MarkerRepository) CreateMarker(
	ctx context.Context, m *database.Marker,
) error {
	if m.UID == "" {
		m.UID = NewMarkerUID()
	}
	if m.Type == "" {
		m.Type = markerTypeDefault
	}
	subject := nullableString(m.SubjectUID)
	row := r.pool.QueryRow(ctx,
		`INSERT INTO markers (
			uid, photo_uid, subject_uid, type,
			x, y, w, h, score, invalid, reviewed
		 ) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9, $10, $11
		 )
		 RETURNING created_at, updated_at`,
		m.UID, m.PhotoUID, subject, m.Type,
		m.X, m.Y, m.W, m.H, m.Score, m.Invalid, m.Reviewed,
	)
	if err := row.Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		return fmt.Errorf("create marker: %w", err)
	}
	return nil
}

// UpdateMarker writes the supplied marker back to the database. All
// editable columns are overwritten; created_at is not modified.
// updated_at is bumped to NOW() by the statement and copied back into m.
// Returns database.ErrNotFound when the row does not exist.
func (r *MarkerRepository) UpdateMarker(
	ctx context.Context, m *database.Marker,
) error {
	if m.Type == "" {
		m.Type = markerTypeDefault
	}
	subject := nullableString(m.SubjectUID)
	row := r.pool.QueryRow(ctx,
		`UPDATE markers SET
			photo_uid = $1, subject_uid = $2, type = $3,
			x = $4, y = $5, w = $6, h = $7,
			score = $8, invalid = $9, reviewed = $10,
			updated_at = NOW()
		 WHERE uid = $11
		 RETURNING updated_at`,
		m.PhotoUID, subject, m.Type,
		m.X, m.Y, m.W, m.H,
		m.Score, m.Invalid, m.Reviewed,
		m.UID,
	)
	if err := row.Scan(&m.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("update marker: %w", err)
	}
	return nil
}

// DeleteMarker hard-deletes the marker row. Returns database.ErrNotFound
// when no row is affected.
func (r *MarkerRepository) DeleteMarker(
	ctx context.Context, uid string,
) error {
	res, err := r.pool.Exec(ctx, `DELETE FROM markers WHERE uid = $1`, uid)
	if err != nil {
		return fmt.Errorf("delete marker: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete marker rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// AssignSubject sets the marker's subject_uid. Returns
// database.ErrNotFound when the marker does not exist.
func (r *MarkerRepository) AssignSubject(
	ctx context.Context, markerUID, subjectUID string,
) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE markers SET subject_uid = $1, updated_at = NOW()
		 WHERE uid = $2`,
		nullableString(subjectUID), markerUID)
	if err != nil {
		return fmt.Errorf("assign subject: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("assign subject rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// UnassignSubject clears the marker's subject_uid. Returns
// database.ErrNotFound when the marker does not exist.
func (r *MarkerRepository) UnassignSubject(
	ctx context.Context, markerUID string,
) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE markers SET subject_uid = NULL, updated_at = NOW()
		 WHERE uid = $1`, markerUID)
	if err != nil {
		return fmt.Errorf("unassign subject: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("unassign subject rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// SetInvalid toggles the invalid flag on a marker. Invalid markers are
// excluded from subject photo/face counts (see subjects.go) so this is
// how a reviewer surfaces a false positive without permanently deleting
// the bounding box. Returns database.ErrNotFound when the marker does
// not exist.
func (r *MarkerRepository) SetInvalid(
	ctx context.Context, markerUID string, invalid bool,
) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE markers SET invalid = $1, updated_at = NOW()
		 WHERE uid = $2`, invalid, markerUID)
	if err != nil {
		return fmt.Errorf("set invalid: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set invalid rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// --- Helpers ---

// scanMarker reads one markers row using the column order in
// markerColumns.
func scanMarker(s rowScanner) (*database.Marker, error) {
	var m database.Marker
	if err := s.Scan(
		&m.UID, &m.PhotoUID, &m.SubjectUID,
		&m.Type, &m.X, &m.Y, &m.W, &m.H, &m.Score, &m.Invalid, &m.Reviewed,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan marker row: %w", err)
	}
	return &m, nil
}

// Verify interface compliance.
var (
	_ database.MarkerReader = (*MarkerRepository)(nil)
	_ database.MarkerWriter = (*MarkerRepository)(nil)
)
