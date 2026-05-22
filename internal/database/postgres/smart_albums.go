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
)

const (
	// smartAlbumUIDPrefix is the single-character type prefix for smart
	// album UIDs. Picked to be visually distinct from "a" (regular albums)
	// while still encoding the concept.
	smartAlbumUIDPrefix = "s"

	// smartAlbumUIDRandLen is the number of random base32 characters
	// appended after the prefix in a generated smart album UID.
	smartAlbumUIDRandLen = 16
)

// NewSmartAlbumUID returns a freshly generated smart album UID in the same
// `"<prefix>" + 16 lowercase base32 chars` format used elsewhere in this
// package. The random suffix is drawn from crypto/rand; the function
// panics if the system random source fails, since callers cannot
// meaningfully recover.
func NewSmartAlbumUID() string {
	randBytes := (smartAlbumUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("smart album uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return smartAlbumUIDPrefix + strings.ToLower(enc[:smartAlbumUIDRandLen])
}

// smartAlbumColumns is the canonical column list for SELECT statements
// against the smart_albums table.
const smartAlbumColumns = `uid, name, filters, created_at, updated_at, created_by_user_uid`

// SmartAlbumRepository provides PostgreSQL-backed storage for smart albums.
// It implements database.SmartAlbumReader and database.SmartAlbumWriter.
type SmartAlbumRepository struct {
	pool *Pool
}

// NewSmartAlbumRepository returns a SmartAlbumRepository bound to the given
// pool.
func NewSmartAlbumRepository(pool *Pool) *SmartAlbumRepository {
	return &SmartAlbumRepository{pool: pool}
}

// GetSmartAlbum fetches a single smart album by UID. Returns
// database.ErrNotFound when the row does not exist.
func (r *SmartAlbumRepository) GetSmartAlbum(
	ctx context.Context, uid string,
) (*database.SmartAlbum, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+smartAlbumColumns+` FROM smart_albums WHERE uid = $1`, uid)
	album, err := scanSmartAlbum(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get smart album: %w", err)
	}
	return album, nil
}

// ListSmartAlbums returns every smart album, ordered by created_at DESC.
func (r *SmartAlbumRepository) ListSmartAlbums(
	ctx context.Context,
) ([]database.SmartAlbum, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+smartAlbumColumns+`
		 FROM smart_albums
		 ORDER BY created_at DESC, uid ASC`)
	if err != nil {
		return nil, fmt.Errorf("list smart albums: %w", err)
	}
	defer rows.Close()

	var albums []database.SmartAlbum
	for rows.Next() {
		a, err := scanSmartAlbum(rows)
		if err != nil {
			return nil, fmt.Errorf("scan smart album: %w", err)
		}
		albums = append(albums, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate smart albums: %w", err)
	}
	return albums, nil
}

// CreateSmartAlbum inserts a new smart album. UID is generated when empty.
// The created_at/updated_at returned by the database are written back into
// the supplied struct so the caller can include them in the API response.
func (r *SmartAlbumRepository) CreateSmartAlbum(
	ctx context.Context, album *database.SmartAlbum,
) error {
	if album.UID == "" {
		album.UID = NewSmartAlbumUID()
	}
	filtersJSON, err := marshalFilters(album.Filters)
	if err != nil {
		return err
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO smart_albums
		    (uid, name, filters, created_by_user_uid)
		 VALUES ($1, $2, $3::jsonb, $4)
		 RETURNING created_at, updated_at`,
		album.UID, album.Name, filtersJSON, album.CreatedByUserUID,
	)
	if err := row.Scan(&album.CreatedAt, &album.UpdatedAt); err != nil {
		return fmt.Errorf("create smart album: %w", err)
	}
	return nil
}

// UpdateSmartAlbum updates name and filters on an existing smart album.
// Returns ErrNotFound when no row matches the supplied UID. The
// updated_at column is bumped on every successful update.
func (r *SmartAlbumRepository) UpdateSmartAlbum(
	ctx context.Context, album *database.SmartAlbum,
) error {
	filtersJSON, err := marshalFilters(album.Filters)
	if err != nil {
		return err
	}
	row := r.pool.QueryRow(ctx,
		`UPDATE smart_albums
		    SET name = $2, filters = $3::jsonb, updated_at = NOW()
		  WHERE uid = $1
		 RETURNING created_at, updated_at`,
		album.UID, album.Name, filtersJSON,
	)
	err = row.Scan(&album.CreatedAt, &album.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return database.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update smart album: %w", err)
	}
	return nil
}

// DeleteSmartAlbum removes the smart album identified by uid. Returns
// ErrNotFound when no row was deleted.
func (r *SmartAlbumRepository) DeleteSmartAlbum(
	ctx context.Context, uid string,
) error {
	res, err := r.pool.Exec(ctx,
		`DELETE FROM smart_albums WHERE uid = $1`, uid)
	if err != nil {
		return fmt.Errorf("delete smart album: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete smart album rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// marshalFilters serialises the filters map for the JSONB column. A nil
// map is stored as `{}` so SELECTs always return a non-NULL JSON object.
func marshalFilters(filters map[string]any) ([]byte, error) {
	if filters == nil {
		filters = map[string]any{}
	}
	b, err := json.Marshal(filters)
	if err != nil {
		return nil, fmt.Errorf("marshal filters: %w", err)
	}
	return b, nil
}

// scanSmartAlbum reads one smart_albums row using the column order in
// smartAlbumColumns and unmarshals the JSONB filters blob.
func scanSmartAlbum(s rowScanner) (*database.SmartAlbum, error) {
	var (
		a           database.SmartAlbum
		filtersJSON []byte
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := s.Scan(
		&a.UID, &a.Name, &filtersJSON, &createdAt, &updatedAt, &a.CreatedByUserUID,
	); err != nil {
		return nil, fmt.Errorf("scan smart album row: %w", err)
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	a.Filters = map[string]any{}
	if len(filtersJSON) > 0 {
		if err := json.Unmarshal(filtersJSON, &a.Filters); err != nil {
			return nil, fmt.Errorf("unmarshal smart album filters: %w", err)
		}
	}
	return &a, nil
}

// Verify interface compliance.
var (
	_ database.SmartAlbumReader = (*SmartAlbumRepository)(nil)
	_ database.SmartAlbumWriter = (*SmartAlbumRepository)(nil)
)
