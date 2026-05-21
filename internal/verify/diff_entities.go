package verify

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
)

// ppSubjectFull is the rich PhotoPrism subjects projection used by the
// field-diff pass: bio / about / alias / type / favorite / private on
// top of the (uid, name) pair the structural pass already consumes.
type ppSubjectFull struct {
	UID      string
	Name     string
	Type     string
	Bio      string
	About    string
	Alias    string
	Favorite bool
	Private  bool
}

// diffSubjectFields compares the bio/about/alias/type/favorite/private
// columns for each PhotoPrism subject that has a matching native
// subject (resolved by accent-insensitive normalised name).
func (v *verifier) diffSubjectFields(ctx context.Context) error {
	subjects, err := v.readPPSubjectsFull(ctx)
	if err != nil {
		return fmt.Errorf("read pp subjects full: %w", err)
	}
	c := newFieldCollector("subject", &v.report.Subjects.FieldDiffs)
	for i := range subjects {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("subjects field diff canceled: %w", err)
		}
		s := &subjects[i]
		native, err := v.opts.Subjects.GetSubjectByName(ctx, s.Name)
		if err != nil || native == nil {
			// Already reported as MissingInSorter by the structural pass.
			continue
		}
		key := facematch.NormalizePersonName(s.Name)
		compareSubjectRow(c, key, s, native)
	}
	return nil
}

// compareSubjectRow drives every per-field comparison for one matched
// (pp, native) subject pair.
func compareSubjectRow(c *fieldDiffCollector, key string, pp *ppSubjectFull, native *database.Subject) {
	if a, b := normaliseString(pp.Bio), normaliseString(native.Bio); a != b {
		c.Push(key, "bio", a, b)
	}
	if a, b := normaliseString(pp.About), normaliseString(native.About); a != b {
		c.Push(key, "about", a, b)
	}
	if a, b := normaliseString(pp.Alias), normaliseString(native.Alias); a != b {
		c.Push(key, "alias", a, b)
	}
	if pp.Favorite != native.Favorite {
		c.Push(key, "favorite", formatBool(pp.Favorite), formatBool(native.Favorite))
	}
	if pp.Private != native.Private {
		c.Push(key, "private", formatBool(pp.Private), formatBool(native.Private))
	}
	if a, b := normaliseSubjectType(pp.Type), normaliseString(native.Type); a != b {
		c.Push(key, "type", a, b)
	}
}

// normaliseSubjectType folds the PhotoPrism subject type onto the
// native value range. PhotoPrism uses "person" / "pet" / "object" /
// empty; native defaults to "person" when empty.
func normaliseSubjectType(t string) string {
	t = normaliseString(t)
	if t == "" {
		return "person"
	}
	return t
}

// readPPSubjectsFull loads the rich projection: every PhotoPrism
// subject's name, type, bio, about, alias, favorite, private. Anonymous
// (empty-name) rows are excluded — they have no native counterpart by
// design.
func (v *verifier) readPPSubjectsFull(ctx context.Context) ([]ppSubjectFull, error) {
	const query = `
		SELECT subj_uid, subj_name, COALESCE(subj_type, ''),
		       COALESCE(subj_bio, ''),
		       COALESCE(subj_about, ''),
		       COALESCE(subj_alias, ''),
		       COALESCE(subj_favorite, 0), COALESCE(subj_private, 0)
		FROM subjects
		WHERE deleted_at IS NULL AND subj_name <> ''`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pp subjects full: %w", err)
	}
	defer rows.Close()
	var out []ppSubjectFull
	for rows.Next() {
		var (
			s         ppSubjectFull
			fav, priv int
			bio       []byte
			about     []byte
			alias     []byte
			typeRaw   []byte
		)
		if err := rows.Scan(
			&s.UID, &s.Name, &typeRaw,
			&bio, &about, &alias,
			&fav, &priv,
		); err != nil {
			return nil, fmt.Errorf("scan pp subject full: %w", err)
		}
		s.Type = string(typeRaw)
		s.Bio = string(bio)
		s.About = string(about)
		s.Alias = string(alias)
		s.Favorite = fav != 0
		s.Private = priv != 0
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp subjects full: %w", err)
	}
	return out, nil
}

// ppLabelFull is the rich PhotoPrism labels projection used by the
// field-diff pass.
type ppLabelFull struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	Categories  []string
	Priority    int
	Favorite    bool
}

// diffLabelFields compares description / categories / priority /
// favorite for each PhotoPrism label that has a matching native label
// (resolved by slug).
func (v *verifier) diffLabelFields(ctx context.Context) error {
	labels, err := v.readPPLabelsFull(ctx)
	if err != nil {
		return fmt.Errorf("read pp labels full: %w", err)
	}
	c := newFieldCollector("label", &v.report.Labels.FieldDiffs)
	for i := range labels {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("labels field diff canceled: %w", err)
		}
		l := &labels[i]
		if l.Slug == "" {
			continue
		}
		native, err := v.opts.Labels.GetLabelBySlug(ctx, l.Slug)
		if err != nil || native == nil {
			continue
		}
		compareLabelRow(c, l.Slug, l, native)
	}
	return nil
}

// compareLabelRow drives every per-field comparison for one matched
// (pp, native) label pair.
func compareLabelRow(c *fieldDiffCollector, key string, pp *ppLabelFull, native *database.Label) {
	if a, b := normaliseString(pp.Description), normaliseString(native.Description); a != b {
		c.Push(key, "description", a, b)
	}
	src := normaliseStringSlice(pp.Categories)
	dst := normaliseStringSlice(native.Categories)
	if !stringSliceEq(src, dst) {
		c.Push(key, "categories", formatStringSlice(src), formatStringSlice(dst))
	}
	if pp.Priority != native.Priority {
		c.Push(key, "priority", strconv.Itoa(pp.Priority), strconv.Itoa(native.Priority))
	}
	if pp.Favorite != native.Favorite {
		c.Push(key, "favorite", formatBool(pp.Favorite), formatBool(native.Favorite))
	}
}

// readPPLabelsFull loads the rich PhotoPrism labels projection used by
// the field-diff pass.
func (v *verifier) readPPLabelsFull(ctx context.Context) ([]ppLabelFull, error) {
	const query = `
		SELECT id, label_name, label_slug,
		       COALESCE(label_description, ''),
		       COALESCE(label_categories, ''),
		       COALESCE(label_priority, 0),
		       COALESCE(label_favorite, 0)
		FROM labels
		WHERE deleted_at IS NULL
		  AND COALESCE(label_priority, 0) >= 0
		  AND label_name <> ''`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pp labels full: %w", err)
	}
	defer rows.Close()
	var out []ppLabelFull
	for rows.Next() {
		var (
			l                            ppLabelFull
			fav                          int
			slugRaw, descRaw, categories []byte
		)
		if err := rows.Scan(
			&l.ID, &l.Name, &slugRaw,
			&descRaw, &categories,
			&l.Priority, &fav,
		); err != nil {
			return nil, fmt.Errorf("scan pp label full: %w", err)
		}
		l.Slug = string(slugRaw)
		l.Description = string(descRaw)
		l.Categories = splitCommaTrim(string(categories))
		l.Favorite = fav != 0
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp labels full: %w", err)
	}
	return out, nil
}

// ppAlbumFull is the rich PhotoPrism albums projection used by the
// field-diff pass. Mirrors the migrator's stage_albums projection so
// every cell the migrator copies can be compared.
type ppAlbumFull struct {
	UID         string
	Slug        string
	Title       string
	Description string
	Location    string
	Category    string
	Notes       string
	Filter      string
	Order       string
	Type        string
	Favorite    bool
	Private     bool
}

// diffAlbumFields compares description / location / category / notes /
// filter / order / favorite / private / type for each PhotoPrism album
// that has a matching native album (resolved by slug).
func (v *verifier) diffAlbumFields(ctx context.Context) error {
	albums, err := v.readPPAlbumsFull(ctx)
	if err != nil {
		return fmt.Errorf("read pp albums full: %w", err)
	}
	c := newFieldCollector("album", &v.report.Albums.FieldDiffs)
	for i := range albums {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("albums field diff canceled: %w", err)
		}
		a := &albums[i]
		if a.Slug == "" {
			continue
		}
		native, err := v.opts.Albums.GetAlbumBySlug(ctx, a.Slug)
		if err != nil || native == nil {
			continue
		}
		compareAlbumRow(c, a.Slug, a, native)
	}
	return nil
}

// compareAlbumRow drives every per-field comparison for one matched
// (pp, native) album pair.
func compareAlbumRow(c *fieldDiffCollector, key string, pp *ppAlbumFull, native *database.Album) {
	pairs := []struct {
		field, src, dst string
	}{
		{"description", pp.Description, native.Description},
		{"location", pp.Location, native.Location},
		{"category", pp.Category, native.Category},
		{"notes", pp.Notes, native.Notes},
		{"filter", pp.Filter, native.Filter},
		{"album_order", pp.Order, native.Order},
	}
	for _, p := range pairs {
		a, b := normaliseString(p.src), normaliseString(p.dst)
		if a != b {
			c.Push(key, p.field, a, b)
		}
	}
	if pp.Favorite != native.Favorite {
		c.Push(key, "favorite", formatBool(pp.Favorite), formatBool(native.Favorite))
	}
	if pp.Private != native.Private {
		c.Push(key, "private", formatBool(pp.Private), formatBool(native.Private))
	}
	if a, b := normaliseAlbumType(pp.Type), normaliseString(native.Type); a != b {
		c.Push(key, "type", a, b)
	}
}

// normaliseAlbumType folds PhotoPrism's album_type onto the native
// CHECK constraint domain.
func normaliseAlbumType(t string) string {
	t = normaliseString(t)
	switch t {
	case "album", "folder", "moment", "state", "month":
		return t
	default:
		return "album"
	}
}

// readPPAlbumsFull loads the rich PhotoPrism albums projection.
func (v *verifier) readPPAlbumsFull(ctx context.Context) ([]ppAlbumFull, error) {
	const query = `
		SELECT album_uid, COALESCE(album_slug, ''), COALESCE(album_title, ''),
		       COALESCE(album_description, ''), COALESCE(album_type, ''),
		       COALESCE(album_favorite, 0), COALESCE(album_private, 0),
		       COALESCE(album_location, ''),
		       COALESCE(album_category, ''),
		       COALESCE(album_notes, ''),
		       COALESCE(album_filter, ''),
		       COALESCE(album_order, '')
		FROM albums
		WHERE deleted_at IS NULL AND COALESCE(album_title, '') <> ''`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pp albums full: %w", err)
	}
	defer rows.Close()
	var out []ppAlbumFull
	for rows.Next() {
		var (
			a                          ppAlbumFull
			slug, typeRaw              []byte
			location, category         []byte
			notes, filterRaw, orderRaw []byte
			fav, priv                  int
		)
		if err := rows.Scan(
			&a.UID, &slug, &a.Title,
			&a.Description, &typeRaw,
			&fav, &priv,
			&location, &category, &notes, &filterRaw, &orderRaw,
		); err != nil {
			return nil, fmt.Errorf("scan pp album full: %w", err)
		}
		a.Slug = string(slug)
		a.Type = string(typeRaw)
		a.Favorite = fav != 0
		a.Private = priv != 0
		a.Location = string(location)
		a.Category = string(category)
		a.Notes = string(notes)
		a.Filter = string(filterRaw)
		a.Order = string(orderRaw)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp albums full: %w", err)
	}
	return out, nil
}
