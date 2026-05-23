package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
	pq "github.com/lib/pq"
)

// shareSlugPattern is the validation regex enforced by both the application
// and the CHECK constraint on the album_share_links table. The two MUST
// stay in sync — if you change one, change the other.
var shareSlugPattern = regexp.MustCompile(`^[a-z0-9-]{3,64}$`)

// IsValidShareSlug reports whether slug matches the canonical
// `^[a-z0-9-]{3,64}$` pattern.
func IsValidShareSlug(slug string) bool {
	return shareSlugPattern.MatchString(slug)
}

// shareSlugMaxLen caps the slug at the column width. The CHECK
// constraint enforces the same bound; we leave one byte of headroom for
// the "-N" dedup suffix.
const shareSlugMaxLen = 64

// ShareSlugMaxLen returns the maximum length (in bytes) of a share
// slug, matching both the VARCHAR(64) column and the CHECK constraint.
// Exposed so the handler can size its dedup suffix without re-declaring
// the constant.
func ShareSlugMaxLen() int { return shareSlugMaxLen }

// shareSlugFallback is used when an album title slugifies to a string
// that is either empty or too short to satisfy the 3-char minimum from
// the CHECK constraint.
const shareSlugFallback = "album"

// SlugifyShareTitle converts an album title into a slug suitable for
// the album_share_links table: lowercase ASCII, diacritics stripped,
// non-alphanumerics collapsed into "-", trimmed of leading/trailing
// dashes, capped at shareSlugMaxLen runes, and padded out to the
// minimum 3-rune length when the title would otherwise produce
// something too short. Mirrors slugifyTitle in albums.go but with the
// share table's tighter length cap.
func SlugifyShareTitle(title string) string {
	folded := foldDiacritics(title)
	collapsed := collapseToDashSeparated(strings.ToLower(folded))
	if len(collapsed) > shareSlugMaxLen {
		collapsed = strings.TrimRight(collapsed[:shareSlugMaxLen], "-")
	}
	if len(collapsed) < 3 {
		return shareSlugFallback
	}
	return collapsed
}

// ShareLinkRepository provides PostgreSQL-backed storage for public album
// share links. It implements database.ShareLinkReader and
// database.ShareLinkWriter.
type ShareLinkRepository struct {
	pool *Pool
}

// NewShareLinkRepository returns a ShareLinkRepository bound to the given
// pool.
func NewShareLinkRepository(pool *Pool) *ShareLinkRepository {
	return &ShareLinkRepository{pool: pool}
}

const shareLinkColumns = `slug, album_uid,
	COALESCE(password_hash, '') AS password_hash,
	expires_at, created_at, created_by_user_uid`

// GetShareLink fetches a single share link by slug. Returns
// database.ErrNotFound when no row exists.
func (r *ShareLinkRepository) GetShareLink(
	ctx context.Context, slug string,
) (*database.ShareLink, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+shareLinkColumns+` FROM album_share_links WHERE slug = $1`,
		slug)
	link, err := scanShareLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get share link: %w", err)
	}
	return link, nil
}

// ListShareLinksForAlbum returns every share link pointing at the given
// album, ordered by created_at descending.
func (r *ShareLinkRepository) ListShareLinksForAlbum(
	ctx context.Context, albumUID string,
) ([]database.ShareLink, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+shareLinkColumns+`
		 FROM album_share_links
		 WHERE album_uid = $1
		 ORDER BY created_at DESC, slug ASC`,
		albumUID)
	if err != nil {
		return nil, fmt.Errorf("list share links: %w", err)
	}
	defer rows.Close()

	var links []database.ShareLink
	for rows.Next() {
		link, err := scanShareLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan share link: %w", err)
		}
		links = append(links, *link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate share links: %w", err)
	}
	return links, nil
}

// CreateShareLink inserts a new share link. Returns ErrShareLinkInvalidSlug
// when the slug fails the validation pattern and ErrShareLinkSlugTaken on a
// primary-key collision.
func (r *ShareLinkRepository) CreateShareLink(
	ctx context.Context, link *database.ShareLink,
) error {
	if !IsValidShareSlug(link.Slug) {
		return database.ErrShareLinkInvalidSlug
	}
	passHash := nullableString(link.PasswordHash)
	var expiresAt any
	if link.ExpiresAt != nil {
		expiresAt = link.ExpiresAt.UTC()
	} else {
		expiresAt = nil
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO album_share_links
		    (slug, album_uid, password_hash, expires_at, created_by_user_uid)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		link.Slug, link.AlbumUID, passHash, expiresAt, link.CreatedByUserUID,
	)
	if err := row.Scan(&link.CreatedAt); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return database.ErrShareLinkSlugTaken
		}
		return fmt.Errorf("create share link: %w", err)
	}
	return nil
}

// DeleteShareLink removes the share link identified by slug. Returns
// ErrNotFound when no row was deleted.
func (r *ShareLinkRepository) DeleteShareLink(
	ctx context.Context, slug string,
) error {
	res, err := r.pool.Exec(ctx,
		`DELETE FROM album_share_links WHERE slug = $1`, slug)
	if err != nil {
		return fmt.Errorf("delete share link: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete share link rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// scanShareLink reads one album_share_links row using the column order in
// shareLinkColumns.
func scanShareLink(s rowScanner) (*database.ShareLink, error) {
	var l database.ShareLink
	var expires sql.NullTime
	if err := s.Scan(
		&l.Slug, &l.AlbumUID, &l.PasswordHash,
		&expires, &l.CreatedAt, &l.CreatedByUserUID,
	); err != nil {
		return nil, fmt.Errorf("scan share link row: %w", err)
	}
	if expires.Valid {
		t := expires.Time
		l.ExpiresAt = &t
	}
	return &l, nil
}

// Verify interface compliance.
var (
	_ database.ShareLinkReader = (*ShareLinkRepository)(nil)
	_ database.ShareLinkWriter = (*ShareLinkRepository)(nil)
)
