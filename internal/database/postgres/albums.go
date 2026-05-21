package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/lib/pq"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	// albumTypeDefault is the default value for the albums.type column when
	// the caller does not specify one. It matches the CHECK constraint set
	// in migration 032.
	albumTypeDefault = "album"

	// albumOrderByDefault is the default value for the albums.order_by
	// column when the caller does not specify one.
	albumOrderByDefault = "newest"

	// albumUIDPrefix is the single-character type prefix for album UIDs.
	albumUIDPrefix = "a"

	// albumUIDRandLen is the number of random base32 characters appended
	// after the "a" prefix in a generated album UID.
	albumUIDRandLen = 16

	// albumSlugMaxLen caps generated slugs to fit the VARCHAR(255) column
	// while leaving headroom for the "-N" dedup suffix.
	albumSlugMaxLen = 240

	// defaultAlbumListLimit is the default page size for ListAlbums when
	// AlbumQuery.Limit is 0.
	defaultAlbumListLimit = 100

	// maxAlbumListLimit caps AlbumQuery.Limit; values larger than this are
	// clamped down silently.
	maxAlbumListLimit = 500
)

// albumColumns is the canonical column list for SELECT statements against
// the albums table. The order here matches scanAlbumWithCount below.
// Location/Category/Notes/Filter/AlbumOrder were added by migration 037
// to preserve PhotoPrism's album_location / album_category / album_notes /
// album_filter (smart-album DSL) / album_order through migrate-from-
// photoprism.
const albumColumns = `a.uid, a.slug, a.title, a.description, a.type,
	COALESCE(a.cover_photo_uid, ''),
	a.favorite, a.private, a.order_by,
	COALESCE(a.created_by, ''),
	a.location, a.category, a.notes, a.filter, a.album_order,
	a.created_at, a.updated_at,
	COALESCE((SELECT COUNT(*) FROM album_photos ap WHERE ap.album_uid = a.uid), 0) AS photo_count`

// AlbumRepository provides PostgreSQL-backed storage for albums and their
// photo membership rows. It implements database.AlbumReader and
// database.AlbumWriter.
type AlbumRepository struct {
	pool *Pool
}

// NewAlbumRepository returns an AlbumRepository bound to the given pool.
func NewAlbumRepository(pool *Pool) *AlbumRepository {
	return &AlbumRepository{pool: pool}
}

// NewAlbumUID returns a freshly generated album UID. The format is
// `"a" + 16 lowercase base32 characters`, e.g. "ab3z9k8mq7n2v4xp".
// The random suffix is drawn from crypto/rand; the function panics if the
// system random source fails, since callers cannot meaningfully recover.
func NewAlbumUID() string {
	randBytes := (albumUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("album uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return albumUIDPrefix + strings.ToLower(enc[:albumUIDRandLen])
}

// fallbackSlug is the slug used when a title slugifies to the empty
// string (e.g. an all-punctuation title).
const fallbackSlug = "album"

// slugifyTitle converts an album title into a URL-safe slug: lowercase,
// diacritics stripped, non-alphanumerics collapsed into "-", trimmed of
// leading/trailing dashes, and truncated to albumSlugMaxLen runes.
func slugifyTitle(title string) string {
	folded := foldDiacritics(title)
	collapsed := collapseToDashSeparated(strings.ToLower(folded))
	if len(collapsed) > albumSlugMaxLen {
		collapsed = strings.TrimRight(collapsed[:albumSlugMaxLen], "-")
	}
	if collapsed == "" {
		return fallbackSlug
	}
	return collapsed
}

// foldDiacritics decomposes accented characters and removes the combining
// marks, returning a best-effort ASCII approximation of the input.
func foldDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

// collapseToDashSeparated keeps lowercase ASCII letters and digits, and
// replaces any run of other characters with a single "-". Leading and
// trailing dashes are stripped.
func collapseToDashSeparated(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- Reads ---

// GetAlbum fetches a single album by UID. Returns database.ErrNotFound when
// the row does not exist.
func (r *AlbumRepository) GetAlbum(ctx context.Context, uid string) (*database.Album, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+albumColumns+` FROM albums a WHERE a.uid = $1`, uid)
	a, err := scanAlbum(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get album: %w", err)
	}
	return a, nil
}

// GetAlbumBySlug fetches a single album by slug. Returns
// database.ErrNotFound when the row does not exist.
func (r *AlbumRepository) GetAlbumBySlug(ctx context.Context, slug string) (*database.Album, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+albumColumns+` FROM albums a WHERE a.slug = $1`, slug)
	a, err := scanAlbum(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get album by slug: %w", err)
	}
	return a, nil
}

// ListAlbums returns a page of albums matching the given query.
func (r *AlbumRepository) ListAlbums(
	ctx context.Context, q database.AlbumQuery,
) ([]database.Album, error) {
	where, args := buildAlbumWhere(q)
	orderBy := albumOrderBy(q.SortBy)
	limit, offset := albumPaginationBounds(q)

	listSQL := "SELECT " + albumColumns + " FROM albums a" + where +
		" ORDER BY " + orderBy +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list albums: %w", err)
	}
	defer rows.Close()

	var albums []database.Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, fmt.Errorf("scan album: %w", err)
		}
		albums = append(albums, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate albums: %w", err)
	}
	return albums, nil
}

// ListAlbumPhotoUIDs returns the photo UIDs belonging to the album in
// stored sort_order, then by added_at and photo_uid as deterministic
// tiebreakers.
func (r *AlbumRepository) ListAlbumPhotoUIDs(
	ctx context.Context, albumUID string,
) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT photo_uid FROM album_photos
		 WHERE album_uid = $1
		 ORDER BY sort_order ASC, added_at ASC, photo_uid ASC`,
		albumUID)
	if err != nil {
		return nil, fmt.Errorf("list album photo uids: %w", err)
	}
	defer rows.Close()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan album photo uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate album photo uids: %w", err)
	}
	return uids, nil
}

// ListAlbumsForPhoto returns every album that contains the given photo,
// ordered by created_at descending. Each Album.PhotoCount is populated.
func (r *AlbumRepository) ListAlbumsForPhoto(
	ctx context.Context, photoUID string,
) ([]database.Album, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+albumColumns+` FROM albums a
		 WHERE EXISTS (
		     SELECT 1 FROM album_photos ap
		     WHERE ap.album_uid = a.uid AND ap.photo_uid = $1
		 )
		 ORDER BY a.created_at DESC, a.uid ASC`, photoUID)
	if err != nil {
		return nil, fmt.Errorf("list albums for photo: %w", err)
	}
	defer rows.Close()
	var albums []database.Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, fmt.Errorf("scan album for photo: %w", err)
		}
		albums = append(albums, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate albums for photo: %w", err)
	}
	return albums, nil
}

// --- Writes ---

// CreateAlbum inserts a new album, generating a UID via NewAlbumUID when
// a.UID is empty and a slug via slugifyTitle (with a "-N" dedup suffix) when
// a.Slug is empty. Defaults are applied for the type and order_by columns.
// The created/updated timestamps are set to NOW() by the database and
// copied back into a on success.
func (r *AlbumRepository) CreateAlbum(ctx context.Context, a *database.Album) error {
	if a.UID == "" {
		a.UID = NewAlbumUID()
	}
	if a.Type == "" {
		a.Type = albumTypeDefault
	}
	if a.OrderBy == "" {
		a.OrderBy = albumOrderByDefault
	}
	slug, err := r.resolveSlug(ctx, a.Slug, a.Title, "")
	if err != nil {
		return err
	}
	a.Slug = slug

	cover := nullableString(a.CoverPhotoUID)
	createdBy := nullableString(a.CreatedBy)
	row := r.pool.QueryRow(ctx,
		`INSERT INTO albums (
			uid, slug, title, description, type,
			cover_photo_uid, favorite, private, order_by, created_by,
			location, category, notes, filter, album_order
		 ) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15
		 )
		 RETURNING created_at, updated_at`,
		a.UID, a.Slug, a.Title, a.Description, a.Type,
		cover, a.Favorite, a.Private, a.OrderBy, createdBy,
		a.Location, a.Category, a.Notes, a.Filter, a.Order,
	)
	if err := row.Scan(&a.CreatedAt, &a.UpdatedAt); err != nil {
		return fmt.Errorf("create album: %w", err)
	}
	return nil
}

// UpdateAlbum writes the supplied album back to the database. All editable
// columns are overwritten; created_at is not modified. updated_at is bumped
// to NOW() by the statement and copied back into a. Returns
// database.ErrNotFound when the row does not exist.
func (r *AlbumRepository) UpdateAlbum(ctx context.Context, a *database.Album) error {
	slug, err := r.resolveSlug(ctx, a.Slug, a.Title, a.UID)
	if err != nil {
		return err
	}
	a.Slug = slug
	cover := nullableString(a.CoverPhotoUID)
	createdBy := nullableString(a.CreatedBy)
	row := r.pool.QueryRow(ctx,
		`UPDATE albums SET
			slug = $1, title = $2, description = $3, type = $4,
			cover_photo_uid = $5, favorite = $6, private = $7,
			order_by = $8, created_by = $9,
			location = $10, category = $11, notes = $12,
			filter = $13, album_order = $14,
			updated_at = NOW()
		 WHERE uid = $15
		 RETURNING updated_at`,
		a.Slug, a.Title, a.Description, a.Type,
		cover, a.Favorite, a.Private,
		a.OrderBy, createdBy,
		a.Location, a.Category, a.Notes,
		a.Filter, a.Order,
		a.UID,
	)
	if err := row.Scan(&a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("update album: %w", err)
	}
	return nil
}

// DeleteAlbum hard-deletes the album row; album_photos rows cascade via the
// FK ON DELETE CASCADE clause. Returns database.ErrNotFound when no row is
// affected.
func (r *AlbumRepository) DeleteAlbum(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx, `DELETE FROM albums WHERE uid = $1`, uid)
	if err != nil {
		return fmt.Errorf("delete album: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete album rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// AddPhotos inserts (album_uid, photo_uid) rows for each supplied photo,
// skipping duplicates so a re-add is a silent no-op. Each new row is given
// a sort_order one higher than the current max for the album so the order
// of arrival is preserved.
func (r *AlbumRepository) AddPhotos(
	ctx context.Context, albumUID string, photoUIDs []string,
) error {
	if len(photoUIDs) == 0 {
		return nil
	}
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add photos: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var nextOrder sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(sort_order) FROM album_photos WHERE album_uid = $1`,
		albumUID).Scan(&nextOrder); err != nil {
		return fmt.Errorf("add photos: read max sort_order: %w", err)
	}
	order := int64(0)
	if nextOrder.Valid {
		order = nextOrder.Int64 + 1
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO album_photos (album_uid, photo_uid, sort_order)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (album_uid, photo_uid) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("add photos: prepare insert: %w", err)
	}
	defer stmt.Close()
	for _, photoUID := range photoUIDs {
		res, err := stmt.ExecContext(ctx, albumUID, photoUID, order)
		if err != nil {
			return fmt.Errorf("add photos: insert %s: %w", photoUID, err)
		}
		// Only bump the sort counter when a row was actually inserted so
		// re-adding an existing UID does not leave a gap.
		if n, _ := res.RowsAffected(); n > 0 {
			order++
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add photos: commit: %w", err)
	}
	return nil
}

// RemovePhotos deletes (album_uid, photo_uid) rows for each supplied photo.
// Removing a photo that is not in the album is a silent no-op. If the
// removed photo was the album's cover, the cover is cleared as well.
func (r *AlbumRepository) RemovePhotos(
	ctx context.Context, albumUID string, photoUIDs []string,
) error {
	if len(photoUIDs) == 0 {
		return nil
	}
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("remove photos: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`DELETE FROM album_photos WHERE album_uid = $1 AND photo_uid = $2`)
	if err != nil {
		return fmt.Errorf("remove photos: prepare delete: %w", err)
	}
	defer stmt.Close()
	for _, photoUID := range photoUIDs {
		if _, err := stmt.ExecContext(ctx, albumUID, photoUID); err != nil {
			return fmt.Errorf("remove photos: delete %s: %w", photoUID, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE albums SET cover_photo_uid = NULL, updated_at = NOW()
		 WHERE uid = $1 AND cover_photo_uid = ANY($2)`,
		albumUID, pq.Array(photoUIDs),
	); err != nil {
		return fmt.Errorf("remove photos: clear cover: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("remove photos: commit: %w", err)
	}
	return nil
}

// SetCoverPhoto sets the album's cover photo. Returns ErrNotFound when the
// album does not exist and ErrAlbumPhotoNotInAlbum when the photo is not a
// member of the album.
func (r *AlbumRepository) SetCoverPhoto(
	ctx context.Context, albumUID, photoUID string,
) error {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM albums WHERE uid = $1)`,
		albumUID).Scan(&exists); err != nil {
		return fmt.Errorf("set cover: lookup album: %w", err)
	}
	if !exists {
		return database.ErrNotFound
	}
	var member bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
		     SELECT 1 FROM album_photos
		     WHERE album_uid = $1 AND photo_uid = $2
		 )`, albumUID, photoUID).Scan(&member); err != nil {
		return fmt.Errorf("set cover: lookup membership: %w", err)
	}
	if !member {
		return database.ErrAlbumPhotoNotInAlbum
	}
	if _, err := r.pool.Exec(ctx,
		`UPDATE albums SET cover_photo_uid = $1, updated_at = NOW()
		 WHERE uid = $2`, photoUID, albumUID); err != nil {
		return fmt.Errorf("set cover: update: %w", err)
	}
	return nil
}

// --- Helpers ---

// scanAlbum reads one albums row using the column order in albumColumns.
func scanAlbum(s rowScanner) (*database.Album, error) {
	var a database.Album
	if err := s.Scan(
		&a.UID, &a.Slug, &a.Title, &a.Description, &a.Type,
		&a.CoverPhotoUID,
		&a.Favorite, &a.Private, &a.OrderBy,
		&a.CreatedBy,
		&a.Location, &a.Category, &a.Notes, &a.Filter, &a.Order,
		&a.CreatedAt, &a.UpdatedAt,
		&a.PhotoCount,
	); err != nil {
		return nil, fmt.Errorf("scan album row: %w", err)
	}
	return &a, nil
}

// buildAlbumWhere assembles the WHERE clause + bind args for ListAlbums.
func buildAlbumWhere(q database.AlbumQuery) (string, []any) {
	var clauses []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q.Type != "" {
		clauses = append(clauses, "a.type = "+next(q.Type))
	}
	if q.Favorite != nil {
		clauses = append(clauses, "a.favorite = "+next(*q.Favorite))
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		ph := next("%" + search + "%")
		clauses = append(clauses,
			fmt.Sprintf("(a.title ILIKE %s OR a.description ILIKE %s)", ph, ph))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// albumOrderBy translates the public sort key into a SQL ORDER BY clause.
// The trailing "uid ASC" makes ordering deterministic when keys tie.
func albumOrderBy(sortBy string) string {
	switch sortBy {
	case "title":
		return "a.title ASC, a.uid ASC"
	case "oldest":
		return "a.created_at ASC, a.uid ASC"
	case "photos":
		return "photo_count DESC, a.created_at DESC, a.uid ASC"
	case albumOrderByDefault, "":
		return "a.created_at DESC, a.uid ASC"
	default:
		return "a.created_at DESC, a.uid ASC"
	}
}

// albumPaginationBounds clamps the user-supplied limit/offset into safe
// ranges.
func albumPaginationBounds(q database.AlbumQuery) (int, int) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultAlbumListLimit
	}
	if limit > maxAlbumListLimit {
		limit = maxAlbumListLimit
	}
	offset := max(q.Offset, 0)
	return limit, offset
}

// resolveSlug returns a slug for the given title that is unique among
// albums other than `excludeUID`. If `requested` is non-empty it is used as
// the base; otherwise the title is slugified. Collisions are resolved by
// appending "-2", "-3", ...
func (r *AlbumRepository) resolveSlug(
	ctx context.Context, requested, title, excludeUID string,
) (string, error) {
	base := requested
	if base == "" {
		base = slugifyTitle(title)
	}
	if base == "" {
		base = fallbackSlug
	}
	candidate := base
	for i := 2; ; i++ {
		var exists bool
		err := r.pool.QueryRow(ctx,
			`SELECT EXISTS(
			     SELECT 1 FROM albums WHERE slug = $1 AND uid <> $2
			 )`, candidate, excludeUID).Scan(&exists)
		if err != nil {
			return "", fmt.Errorf("check slug uniqueness: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		suffix := fmt.Sprintf("-%d", i)
		if len(base)+len(suffix) > albumSlugMaxLen {
			candidate = base[:albumSlugMaxLen-len(suffix)] + suffix
		} else {
			candidate = base + suffix
		}
		if i > 10000 {
			return "", fmt.Errorf("resolve slug: too many collisions for %q", base)
		}
	}
}

// Verify interface compliance.
var (
	_ database.AlbumReader = (*AlbumRepository)(nil)
	_ database.AlbumWriter = (*AlbumRepository)(nil)
)
