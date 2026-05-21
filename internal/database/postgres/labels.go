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
	"github.com/lib/pq"
)

const (
	// labelUIDPrefix is the single-character type prefix for label UIDs.
	labelUIDPrefix = "l"

	// labelUIDRandLen is the number of random base32 characters appended
	// after the "l" prefix in a generated label UID.
	labelUIDRandLen = 16

	// labelSlugMaxLen caps generated slugs to fit the VARCHAR(255) column
	// while leaving headroom for the "-N" dedup suffix.
	labelSlugMaxLen = 240

	// defaultLabelListLimit is the default page size for ListLabels when
	// LabelQuery.Limit is 0.
	defaultLabelListLimit = 1000

	// maxLabelListLimit caps LabelQuery.Limit; values larger than this are
	// clamped down silently.
	maxLabelListLimit = 5000

	// labelSlugFallback is the slug used when a name slugifies to the empty
	// string (e.g. an all-punctuation name).
	labelSlugFallback = "label"

	// defaultLabelSource is the value stored in photo_labels.source when the
	// caller does not specify one; matches the CHECK constraint in
	// migration 032.
	defaultLabelSource = "manual"
)

// labelColumns is the canonical column list for SELECT statements against
// the labels table. The order here matches scanLabel below. PhotoCount is
// computed via a correlated subquery so callers do not need a separate
// query to display the per-label photo count. Description and Categories
// were added by migration 037 to preserve PhotoPrism's label_description
// and label_categories during migration.
const labelColumns = `l.uid, l.slug, l.name, l.description, l.categories,
	l.priority, l.favorite,
	l.created_at, l.updated_at,
	COALESCE((SELECT COUNT(*) FROM photo_labels pl WHERE pl.label_uid = l.uid), 0) AS photo_count`

// LabelRepository provides PostgreSQL-backed storage for labels and the
// photo_labels junction. It implements database.LabelReader and
// database.LabelWriter.
type LabelRepository struct {
	pool *Pool
}

// NewLabelRepository returns a LabelRepository bound to the given pool.
func NewLabelRepository(pool *Pool) *LabelRepository {
	return &LabelRepository{pool: pool}
}

// NewLabelUID returns a freshly generated label UID. The format is
// `"l" + 16 lowercase base32 characters`, e.g. "l3z9k8mq7n2v4xpa".
// The random suffix is drawn from crypto/rand; the function panics if the
// system random source fails, since callers cannot meaningfully recover.
func NewLabelUID() string {
	randBytes := (labelUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("label uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return labelUIDPrefix + strings.ToLower(enc[:labelUIDRandLen])
}

// slugifyLabelName converts a label name into a URL-safe slug using the same
// rules as albums: lowercase, diacritics stripped, non-alphanumerics
// collapsed into "-", trimmed of leading/trailing dashes, truncated to
// labelSlugMaxLen runes.
func slugifyLabelName(name string) string {
	folded := foldDiacritics(name)
	collapsed := collapseToDashSeparated(strings.ToLower(folded))
	if len(collapsed) > labelSlugMaxLen {
		collapsed = strings.TrimRight(collapsed[:labelSlugMaxLen], "-")
	}
	if collapsed == "" {
		return labelSlugFallback
	}
	return collapsed
}

// --- Reads ---

// GetLabel fetches a single label by UID. Returns database.ErrNotFound when
// the row does not exist.
func (r *LabelRepository) GetLabel(ctx context.Context, uid string) (*database.Label, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+labelColumns+` FROM labels l WHERE l.uid = $1`, uid)
	label, err := scanLabel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get label: %w", err)
	}
	return label, nil
}

// GetLabelBySlug fetches a single label by slug. Returns
// database.ErrNotFound when the row does not exist.
func (r *LabelRepository) GetLabelBySlug(ctx context.Context, slug string) (*database.Label, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+labelColumns+` FROM labels l WHERE l.slug = $1`, slug)
	label, err := scanLabel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get label by slug: %w", err)
	}
	return label, nil
}

// buildLabelListSQL constructs the SELECT statement for ListLabels and
// returns it along with the bind argument slice. Splitting this out keeps
// ListLabels short enough for the linter while letting the test suite
// inspect the generated SQL in isolation.
func buildLabelListSQL(q database.LabelQuery) (string, []any) {
	where, args := buildLabelWhere(q)
	orderBy := labelOrderBy(q.SortBy)
	limit, offset := labelPaginationBounds(q)
	sql := "SELECT " + labelColumns + " FROM labels l" + where +
		" ORDER BY " + orderBy +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	return sql, args
}

// ListLabels returns a page of labels matching the given query.
func (r *LabelRepository) ListLabels(
	ctx context.Context, q database.LabelQuery,
) ([]database.Label, error) {
	sql, args := buildLabelListSQL(q)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	defer rows.Close()
	labels := make([]database.Label, 0)
	for rows.Next() {
		label, err := scanLabel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		labels = append(labels, *label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labels: %w", err)
	}
	return labels, nil
}

// ListLabelsForPhoto returns every label attached to the given photo,
// ordered by name. Each Label.PhotoCount is populated.
func (r *LabelRepository) ListLabelsForPhoto(
	ctx context.Context, photoUID string,
) ([]database.Label, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+labelColumns+` FROM labels l
		 WHERE EXISTS (
		     SELECT 1 FROM photo_labels pl
		     WHERE pl.label_uid = l.uid AND pl.photo_uid = $1
		 )
		 ORDER BY l.name ASC, l.uid ASC`, photoUID)
	if err != nil {
		return nil, fmt.Errorf("list labels for photo: %w", err)
	}
	defer rows.Close()
	var labels []database.Label
	for rows.Next() {
		label, err := scanLabel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan label for photo: %w", err)
		}
		labels = append(labels, *label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labels for photo: %w", err)
	}
	return labels, nil
}

// --- Writes ---

// EnsureLabel returns the existing label matching the slugified name or
// creates one if it does not yet exist. The single INSERT ... ON CONFLICT
// statement keeps two concurrent callers (e.g. the AI sort pipeline
// processing photos in parallel) from racing to create duplicate rows; the
// returned UID is then projected through the canonical labelColumns list
// in a follow-up SELECT so the photo_count subquery sees the upserted row.
// (A single statement that combines the two cannot work: data-modifying
// statements in a WITH clause share the surrounding snapshot, so the outer
// SELECT does not observe the just-inserted row.)
func (r *LabelRepository) EnsureLabel(
	ctx context.Context, name string,
) (*database.Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("ensure label: name is required")
	}
	slug := slugifyLabelName(name)
	uid := NewLabelUID()
	var resolvedUID string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO labels (uid, slug, name, priority, favorite)
		 VALUES ($1, $2, $3, 0, FALSE)
		 ON CONFLICT (slug) DO UPDATE SET updated_at = NOW()
		 RETURNING uid`,
		uid, slug, name,
	).Scan(&resolvedUID)
	if err != nil {
		return nil, fmt.Errorf("ensure label upsert: %w", err)
	}
	row := r.pool.QueryRow(ctx,
		`SELECT `+labelColumns+` FROM labels l WHERE l.uid = $1`, resolvedUID)
	label, err := scanLabel(row)
	if err != nil {
		return nil, fmt.Errorf("ensure label select: %w", err)
	}
	return label, nil
}

// UpdateLabel writes the supplied label back to the database. All editable
// columns (slug, name, priority, favorite) are overwritten; created_at is
// not modified. updated_at is bumped to NOW() by the statement and copied
// back into l. Returns database.ErrNotFound when the row does not exist.
func (r *LabelRepository) UpdateLabel(ctx context.Context, l *database.Label) error {
	slug, err := r.resolveLabelSlug(ctx, l.Slug, l.Name, l.UID)
	if err != nil {
		return err
	}
	l.Slug = slug
	categories := categoriesArray(l.Categories)
	row := r.pool.QueryRow(ctx,
		`UPDATE labels SET
			slug = $1, name = $2, description = $3, categories = $4,
			priority = $5, favorite = $6,
			updated_at = NOW()
		 WHERE uid = $7
		 RETURNING updated_at`,
		l.Slug, l.Name, l.Description, categories,
		l.Priority, l.Favorite, l.UID,
	)
	if err := row.Scan(&l.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("update label: %w", err)
	}
	return nil
}

// DeleteLabels hard-deletes the labels identified by the supplied UIDs and
// returns the number of rows that were actually removed. photo_labels rows
// cascade via the FK ON DELETE CASCADE clause. Unknown UIDs are silently
// skipped, so the returned count reflects only the rows that existed.
func (r *LabelRepository) DeleteLabels(
	ctx context.Context, uids []string,
) (int, error) {
	if len(uids) == 0 {
		return 0, nil
	}
	res, err := r.pool.Exec(ctx,
		`DELETE FROM labels WHERE uid = ANY($1)`, pq.Array(uids))
	if err != nil {
		return 0, fmt.Errorf("delete labels: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete labels rows affected: %w", err)
	}
	return int(n), nil
}

// AddPhotoLabel attaches a label to a photo, recording the provenance
// (source) and the uncertainty score reported by the caller. Re-adding the
// same (photo, label) pair is a silent no-op enforced by the primary key.
// Source defaults to "manual" when empty.
func (r *LabelRepository) AddPhotoLabel(
	ctx context.Context, photoUID, labelUID, source string, uncertainty int,
) error {
	if source == "" {
		source = defaultLabelSource
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO photo_labels (photo_uid, label_uid, source, uncertainty)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (photo_uid, label_uid) DO NOTHING`,
		photoUID, labelUID, source, uncertainty,
	)
	if err != nil {
		return fmt.Errorf("add photo label: %w", err)
	}
	return nil
}

// RemovePhotoLabel detaches a label from a photo. Removing a label that is
// not attached is a silent no-op.
func (r *LabelRepository) RemovePhotoLabel(
	ctx context.Context, photoUID, labelUID string,
) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM photo_labels WHERE photo_uid = $1 AND label_uid = $2`,
		photoUID, labelUID,
	)
	if err != nil {
		return fmt.Errorf("remove photo label: %w", err)
	}
	return nil
}

// --- Helpers ---

// scanLabel reads one labels row using the column order in labelColumns.
func scanLabel(s rowScanner) (*database.Label, error) {
	var l database.Label
	var categories pq.StringArray
	if err := s.Scan(
		&l.UID, &l.Slug, &l.Name, &l.Description, &categories,
		&l.Priority, &l.Favorite,
		&l.CreatedAt, &l.UpdatedAt,
		&l.PhotoCount,
	); err != nil {
		return nil, fmt.Errorf("scan label row: %w", err)
	}
	if categories == nil {
		l.Categories = []string{}
	} else {
		l.Categories = []string(categories)
	}
	return &l, nil
}

// categoriesArray wraps a string slice into a pq.StringArray suitable for
// binding to a TEXT[] parameter. A nil slice is normalised to an empty
// array so the destination column never receives NULL (the schema is
// NOT NULL with a '{}' default).
func categoriesArray(in []string) pq.StringArray {
	if len(in) == 0 {
		return pq.StringArray{}
	}
	return pq.StringArray(in)
}

// buildLabelWhere assembles the WHERE clause + bind args for ListLabels.
func buildLabelWhere(q database.LabelQuery) (string, []any) {
	var clauses []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		ph := next("%" + search + "%")
		clauses = append(clauses,
			fmt.Sprintf("(l.name ILIKE %s OR l.slug ILIKE %s)", ph, ph))
	}
	if q.MinPhotos > 0 {
		ph := next(q.MinPhotos)
		clauses = append(clauses,
			"(SELECT COUNT(*) FROM photo_labels pl WHERE pl.label_uid = l.uid) >= "+ph)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// labelOrderBy translates the public sort key into a SQL ORDER BY clause.
// The trailing "uid ASC" makes ordering deterministic when keys tie.
func labelOrderBy(sortBy string) string {
	switch sortBy {
	case "-name":
		return "l.name DESC, l.uid ASC"
	case "count":
		return "photo_count ASC, l.name ASC, l.uid ASC"
	case "-count":
		return "photo_count DESC, l.name ASC, l.uid ASC"
	case "name", "":
		return "l.name ASC, l.uid ASC"
	default:
		return "l.name ASC, l.uid ASC"
	}
}

// labelPaginationBounds clamps the user-supplied limit/offset into safe
// ranges.
func labelPaginationBounds(q database.LabelQuery) (int, int) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLabelListLimit
	}
	if limit > maxLabelListLimit {
		limit = maxLabelListLimit
	}
	offset := max(q.Offset, 0)
	return limit, offset
}

// resolveLabelSlug returns a slug for the given name that is unique among
// labels other than `excludeUID`. If `requested` is non-empty it is used as
// the base; otherwise the name is slugified. Collisions are resolved by
// appending "-2", "-3", ...
func (r *LabelRepository) resolveLabelSlug(
	ctx context.Context, requested, name, excludeUID string,
) (string, error) {
	base := requested
	if base == "" {
		base = slugifyLabelName(name)
	}
	if base == "" {
		base = labelSlugFallback
	}
	candidate := base
	for i := 2; ; i++ {
		var exists bool
		err := r.pool.QueryRow(ctx,
			`SELECT EXISTS(
			     SELECT 1 FROM labels WHERE slug = $1 AND uid <> $2
			 )`, candidate, excludeUID).Scan(&exists)
		if err != nil {
			return "", fmt.Errorf("check label slug uniqueness: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		suffix := fmt.Sprintf("-%d", i)
		if len(base)+len(suffix) > labelSlugMaxLen {
			candidate = base[:labelSlugMaxLen-len(suffix)] + suffix
		} else {
			candidate = base + suffix
		}
		if i > 10000 {
			return "", fmt.Errorf("resolve label slug: too many collisions for %q", base)
		}
	}
}

// Verify interface compliance.
var (
	_ database.LabelReader = (*LabelRepository)(nil)
	_ database.LabelWriter = (*LabelRepository)(nil)
)
