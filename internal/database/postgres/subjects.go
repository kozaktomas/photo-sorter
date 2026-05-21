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
	// subjectUIDPrefix is the single-character type prefix for subject UIDs.
	subjectUIDPrefix = "s"

	// subjectUIDRandLen is the number of random base32 characters appended
	// after the "s" prefix in a generated subject UID.
	subjectUIDRandLen = 16

	// subjectSlugMaxLen caps generated slugs to fit the VARCHAR(255) column
	// while leaving headroom for the "-N" dedup suffix.
	subjectSlugMaxLen = 240

	// subjectTypeDefault is the default value for the subjects.type column
	// when the caller does not specify one. It matches the CHECK constraint
	// set in migration 032.
	subjectTypeDefault = "person"

	// subjectSlugFallback is the slug used when a name slugifies to the
	// empty string (e.g. an all-punctuation name).
	subjectSlugFallback = "subject"

	// defaultSubjectListLimit is the default page size for ListSubjects
	// when SubjectQuery.Limit is 0.
	defaultSubjectListLimit = 200

	// maxSubjectListLimit caps SubjectQuery.Limit; values larger than this
	// are clamped down silently.
	maxSubjectListLimit = 1000

	// subjectSortByName is the SortBy value for the default "by name"
	// ordering. Pulled into a constant so goconst does not flag the
	// recurring literal across this file and its sibling test.
	subjectSortByName = "name"
)

// subjectColumns is the canonical column list for SELECT statements
// against the subjects table. The order here matches scanSubject below.
// PhotoCount and FaceCount are computed in the same LEFT JOIN so callers
// do not need a second round-trip per row. Invalid markers are excluded
// from both counts so flagged false positives do not inflate the totals.
const subjectColumns = `s.uid, s.slug, s.name, s.type, s.favorite, s.private,
	s.notes, COALESCE(s.cover_photo_uid, ''),
	s.created_at, s.updated_at,
	COALESCE(c.photo_count, 0) AS photo_count,
	COALESCE(c.face_count, 0)  AS face_count`

// subjectCountJoin is the LEFT JOIN that computes per-subject photo and
// face counts from the markers table. Markers flagged invalid are excluded
// so a reviewer-marked false positive does not raise either count.
const subjectCountJoin = `LEFT JOIN (
	SELECT subject_uid,
	       COUNT(DISTINCT photo_uid) AS photo_count,
	       COUNT(*)                  AS face_count
	FROM markers
	WHERE subject_uid IS NOT NULL AND invalid = FALSE
	GROUP BY subject_uid
) c ON c.subject_uid = s.uid`

// SubjectRepository provides PostgreSQL-backed storage for subjects.
// It implements database.SubjectReader and database.SubjectWriter.
type SubjectRepository struct {
	pool *Pool
}

// NewSubjectRepository returns a SubjectRepository bound to the given
// pool.
func NewSubjectRepository(pool *Pool) *SubjectRepository {
	return &SubjectRepository{pool: pool}
}

// NewSubjectUID returns a freshly generated subject UID. The format is
// `"s" + 16 lowercase base32 characters`, e.g. "s3z9k8mq7n2v4xpa".
// The random suffix is drawn from crypto/rand; the function panics if
// the system random source fails, since callers cannot meaningfully
// recover.
func NewSubjectUID() string {
	randBytes := (subjectUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("subject uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return subjectUIDPrefix + strings.ToLower(enc[:subjectUIDRandLen])
}

// slugifySubjectName converts a subject name into a URL-safe slug using
// the same rules as albums/labels: lowercase, diacritics stripped,
// non-alphanumerics collapsed into "-", trimmed of leading/trailing
// dashes, truncated to subjectSlugMaxLen runes.
func slugifySubjectName(name string) string {
	folded := foldDiacritics(name)
	collapsed := collapseToDashSeparated(strings.ToLower(folded))
	if len(collapsed) > subjectSlugMaxLen {
		collapsed = strings.TrimRight(collapsed[:subjectSlugMaxLen], "-")
	}
	if collapsed == "" {
		return subjectSlugFallback
	}
	return collapsed
}

// --- Reads ---

// GetSubject fetches a single subject by UID. Returns
// database.ErrNotFound when the row does not exist.
func (r *SubjectRepository) GetSubject(
	ctx context.Context, uid string,
) (*database.Subject, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+subjectColumns+` FROM subjects s `+subjectCountJoin+
			` WHERE s.uid = $1`, uid)
	s, err := scanSubject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get subject: %w", err)
	}
	return s, nil
}

// GetSubjectByName fetches a subject by name, normalising via the
// unaccent extension + lower(). Accent and case variants therefore map
// to the same row ("Tomáš" == "tomas" == "Tomas"). Returns
// database.ErrNotFound when no row matches.
func (r *SubjectRepository) GetSubjectByName(
	ctx context.Context, name string,
) (*database.Subject, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+subjectColumns+` FROM subjects s `+subjectCountJoin+
			` WHERE lower(unaccent(s.name)) = lower(unaccent($1))`, name)
	s, err := scanSubject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get subject by name: %w", err)
	}
	return s, nil
}

// ListSubjects returns a page of subjects matching the given query.
// Counts are populated via the same LEFT JOIN as GetSubject.
func (r *SubjectRepository) ListSubjects(
	ctx context.Context, q database.SubjectQuery,
) ([]database.Subject, error) {
	where, args := buildSubjectWhere(q)
	orderBy := subjectOrderBy(q.SortBy)
	limit, offset := subjectPaginationBounds(q)

	listSQL := "SELECT " + subjectColumns +
		" FROM subjects s " + subjectCountJoin +
		where +
		" ORDER BY " + orderBy +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	defer rows.Close()

	subjects := make([]database.Subject, 0)
	for rows.Next() {
		s, err := scanSubject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subject: %w", err)
		}
		subjects = append(subjects, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subjects: %w", err)
	}
	return subjects, nil
}

// ListSubjectsForPhoto returns every subject that has at least one valid
// (non-invalid) marker on the given photo, ordered by name. Each
// Subject.PhotoCount / FaceCount are populated.
func (r *SubjectRepository) ListSubjectsForPhoto(
	ctx context.Context, photoUID string,
) ([]database.Subject, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+subjectColumns+`
		 FROM subjects s `+subjectCountJoin+`
		 WHERE EXISTS (
		     SELECT 1 FROM markers m
		     WHERE m.subject_uid = s.uid
		       AND m.photo_uid = $1
		       AND m.invalid = FALSE
		 )
		 ORDER BY s.name ASC, s.uid ASC`, photoUID)
	if err != nil {
		return nil, fmt.Errorf("list subjects for photo: %w", err)
	}
	defer rows.Close()
	var subjects []database.Subject
	for rows.Next() {
		s, err := scanSubject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subject for photo: %w", err)
		}
		subjects = append(subjects, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subjects for photo: %w", err)
	}
	return subjects, nil
}

// --- Writes ---

// EnsureSubject returns the existing subject matching the
// accent-insensitive lowercased name or creates one if it does not yet
// exist. The lookup-then-insert dance is wrapped in a single transaction
// with FOR UPDATE on the existence check so two concurrent callers
// cannot race-create duplicate rows. A separate slug is generated from
// the name; collisions on the slug index trigger a fallback to a "-N"
// suffix.
func (r *SubjectRepository) EnsureSubject(
	ctx context.Context, name, subjectType string,
) (*database.Subject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("ensure subject: name is required")
	}
	if subjectType == "" {
		subjectType = subjectTypeDefault
	}

	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("ensure subject: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	uid, err := ensureSubjectTx(ctx, tx, name, subjectType)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("ensure subject: commit: %w", err)
	}
	return r.GetSubject(ctx, uid)
}

// ensureSubjectTx is the body of EnsureSubject extracted into a helper
// so the public method stays under the cyclomatic-complexity limit.
// Returns the UID of the (existing or newly inserted) row; the caller
// is responsible for committing the surrounding transaction.
func ensureSubjectTx(
	ctx context.Context, tx *sql.Tx, name, subjectType string,
) (string, error) {
	var existingUID string
	err := tx.QueryRowContext(ctx,
		`SELECT uid FROM subjects
		 WHERE lower(unaccent(name)) = lower(unaccent($1))
		 LIMIT 1 FOR UPDATE`, name).Scan(&existingUID)
	if err == nil {
		return existingUID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("ensure subject: lookup: %w", err)
	}

	slug, err := resolveSubjectSlugTx(ctx, tx, "", name, "")
	if err != nil {
		return "", err
	}
	uid := NewSubjectUID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subjects (uid, slug, name, type)
		 VALUES ($1, $2, $3, $4)`,
		uid, slug, name, subjectType,
	); err != nil {
		return "", fmt.Errorf("ensure subject: insert: %w", err)
	}
	return uid, nil
}

// UpdateSubject writes the supplied subject back to the database. All
// editable columns are overwritten; created_at is not modified.
// updated_at is bumped to NOW() by the statement and copied back into s.
// Returns database.ErrNotFound when the row does not exist. The slug is
// re-resolved (re-slugged + collision suffix) when s.Slug is empty.
func (r *SubjectRepository) UpdateSubject(
	ctx context.Context, s *database.Subject,
) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update subject: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	slug, err := resolveSubjectSlugTx(ctx, tx, s.Slug, s.Name, s.UID)
	if err != nil {
		return err
	}
	s.Slug = slug
	subjectType := s.Type
	if subjectType == "" {
		subjectType = subjectTypeDefault
	}
	s.Type = subjectType
	cover := nullableString(s.CoverPhotoUID)
	row := tx.QueryRowContext(ctx,
		`UPDATE subjects SET
			slug = $1, name = $2, type = $3, favorite = $4, private = $5,
			notes = $6, cover_photo_uid = $7, updated_at = NOW()
		 WHERE uid = $8
		 RETURNING updated_at`,
		s.Slug, s.Name, s.Type, s.Favorite, s.Private,
		s.Notes, cover, s.UID,
	)
	if err := row.Scan(&s.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("update subject: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update subject: commit: %w", err)
	}
	return nil
}

// DeleteSubject hard-deletes the subject row. Markers that reference the
// subject have their subject_uid set to NULL via the FK ON DELETE SET
// NULL clause (the markers themselves are preserved). Returns
// database.ErrNotFound when no row is affected.
func (r *SubjectRepository) DeleteSubject(
	ctx context.Context, uid string,
) error {
	res, err := r.pool.Exec(ctx, `DELETE FROM subjects WHERE uid = $1`, uid)
	if err != nil {
		return fmt.Errorf("delete subject: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete subject rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// --- Helpers ---

// scanSubject reads one subjects row using the column order in
// subjectColumns.
func scanSubject(s rowScanner) (*database.Subject, error) {
	var subj database.Subject
	if err := s.Scan(
		&subj.UID, &subj.Slug, &subj.Name, &subj.Type, &subj.Favorite, &subj.Private,
		&subj.Notes, &subj.CoverPhotoUID,
		&subj.CreatedAt, &subj.UpdatedAt,
		&subj.PhotoCount, &subj.FaceCount,
	); err != nil {
		return nil, fmt.Errorf("scan subject row: %w", err)
	}
	return &subj, nil
}

// buildSubjectWhere assembles the WHERE clause + bind args for
// ListSubjects.
func buildSubjectWhere(q database.SubjectQuery) (string, []any) {
	var clauses []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q.Type != "" {
		clauses = append(clauses, "s.type = "+next(q.Type))
	}
	if q.Favorite != nil {
		clauses = append(clauses, "s.favorite = "+next(*q.Favorite))
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		// Accent-insensitive ILIKE on both name and notes so users can
		// type "Tomas" and match "Tomáš".
		ph := next("%" + search + "%")
		clauses = append(clauses, fmt.Sprintf(
			"(unaccent(s.name) ILIKE unaccent(%s) OR unaccent(s.notes) ILIKE unaccent(%s))",
			ph, ph))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// subjectOrderBy translates the public sort key into a SQL ORDER BY
// clause. The trailing "uid ASC" makes ordering deterministic when keys
// tie.
func subjectOrderBy(sortBy string) string {
	switch sortBy {
	case "photos":
		return "photo_count DESC, s.name ASC, s.uid ASC"
	case "newest":
		return "s.created_at DESC, s.uid ASC"
	case subjectSortByName, "":
		return "s.name ASC, s.uid ASC"
	default:
		return "s.name ASC, s.uid ASC"
	}
}

// subjectPaginationBounds clamps the user-supplied limit/offset into
// safe ranges.
func subjectPaginationBounds(q database.SubjectQuery) (int, int) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultSubjectListLimit
	}
	if limit > maxSubjectListLimit {
		limit = maxSubjectListLimit
	}
	offset := max(q.Offset, 0)
	return limit, offset
}

// resolveSubjectSlugTx returns a slug for the given name that is unique
// among subjects other than excludeUID. If requested is non-empty it is
// used as the base; otherwise the name is slugified. Collisions are
// resolved by appending "-2", "-3", ... All checks run on the supplied
// transaction so the surrounding INSERT/UPDATE sees a consistent view.
func resolveSubjectSlugTx(
	ctx context.Context, tx *sql.Tx, requested, name, excludeUID string,
) (string, error) {
	base := requested
	if base == "" {
		base = slugifySubjectName(name)
	}
	if base == "" {
		base = subjectSlugFallback
	}
	candidate := base
	for i := 2; ; i++ {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(
			     SELECT 1 FROM subjects WHERE slug = $1 AND uid <> $2
			 )`, candidate, excludeUID).Scan(&exists); err != nil {
			return "", fmt.Errorf("check subject slug uniqueness: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		suffix := fmt.Sprintf("-%d", i)
		if len(base)+len(suffix) > subjectSlugMaxLen {
			candidate = base[:subjectSlugMaxLen-len(suffix)] + suffix
		} else {
			candidate = base + suffix
		}
		if i > 10000 {
			return "", fmt.Errorf("resolve subject slug: too many collisions for %q", base)
		}
	}
}

// Verify interface compliance.
var (
	_ database.SubjectReader = (*SubjectRepository)(nil)
	_ database.SubjectWriter = (*SubjectRepository)(nil)
)
