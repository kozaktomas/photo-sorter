package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppAlbum is a single row of PhotoPrism's `albums` table. Location /
// Category / Notes / Filter / Order were added by task 332a727c to plug
// data loss for smart albums and locality metadata; album_filter is
// preserved verbatim because photo-sorter has no smart-album evaluator
// yet (see the comment on stage_albums.processOneAlbum).
type ppAlbum struct {
	UID         string
	Slug        string
	Title       string
	Description string
	Type        string
	Location    string
	Category    string
	Notes       string
	Filter      string
	Order       string
	Favorite    bool
	Private     bool
}

// ppAlbumPhoto is the projection of `photos_albums` joined back to the
// PhotoPrism photo UID, so we can route it through photoMap.
type ppAlbumPhoto struct {
	AlbumUID string
	PhotoUID string
}

// stageAlbums imports albums and their photo memberships. CreateAlbum is
// idempotent by slug — we use GetAlbumBySlug to detect re-runs. The
// album_type column maps directly to native types when valid; everything
// else collapses to "album" (the native default).
func (m *migrator) stageAlbums(ctx context.Context) error {
	albums, err := m.readPPAlbums(ctx)
	if err != nil {
		return fmt.Errorf("read albums: %w", err)
	}
	memberships, err := m.readPPAlbumPhotos(ctx)
	if err != nil {
		return fmt.Errorf("read photos_albums: %w", err)
	}

	uidMap, err := m.upsertAlbums(ctx, albums)
	if err != nil {
		return err
	}
	if len(memberships) == 0 {
		return nil
	}
	return m.linkAlbumPhotos(ctx, memberships, uidMap)
}

// upsertAlbums inserts (or finds) every PhotoPrism album in the native
// store and returns the (pp-album-uid → native-album-uid) mapping.
func (m *migrator) upsertAlbums(
	ctx context.Context, albums []ppAlbum,
) (map[string]string, error) {
	uidMap := make(map[string]string, len(albums))
	summary := StageSummary{Stage: StageAlbums, Read: len(albums)}
	bar := newStageBar(len(albums), "albums")
	defer finishBar(bar)
	for i := range albums {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(summary)
			return uidMap, fmt.Errorf("albums canceled: %w", err)
		}
		_ = bar.Add(1)
		m.processOneAlbum(ctx, &albums[i], uidMap, &summary)
	}
	m.report.AppendStage(summary)
	return uidMap, nil
}

// processOneAlbum upserts a single album and records its native UID in
// uidMap. Existing rows are counted as Skipped so re-runs report zero
// creations — but the location/category/notes/filter/order columns are
// backfilled when the destination value is still the column zero value
// (task 332a727c).
//
// album_filter is preserved verbatim — photo-sorter has no smart-album
// evaluator yet, so the field is informational-only for the time being.
// Smart albums (album_type != 'album', e.g. month/moment/state) are
// migrated as regular albums with their album_filter intact so the
// operator can audit them and a future smart-album feature can consume
// the DSL.
func (m *migrator) processOneAlbum(
	ctx context.Context, a *ppAlbum, uidMap map[string]string, summary *StageSummary,
) {
	if m.opts.DryRun {
		summary.Created++
		uidMap[a.UID] = "dry-album-" + a.Slug
		return
	}
	if a.Slug != "" {
		existing, err := m.opts.Albums.GetAlbumBySlug(ctx, a.Slug)
		if err == nil && existing != nil {
			uidMap[a.UID] = existing.UID
			m.backfillAlbumExtras(ctx, a, existing)
			summary.Skipped++
			return
		}
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			fmt.Fprintf(m.out, "\nalbum %q lookup: %v\n", a.Title, err)
			summary.Failed++
			return
		}
	}
	native := &database.Album{
		// Preserve the PhotoPrism album_uid as the native album.uid so any
		// pre-existing native rows referencing this album by PhotoPrism UID
		// stay valid without a remap pass. CreateAlbum keeps a non-empty
		// UID; only blank values trigger NewAlbumUID().
		UID:         a.UID,
		Slug:        a.Slug,
		Title:       a.Title,
		Description: a.Description,
		Type:        normaliseAlbumType(a.Type),
		Favorite:    a.Favorite,
		Private:     a.Private,
		Location:    a.Location,
		Category:    a.Category,
		Notes:       a.Notes,
		Filter:      a.Filter,
		Order:       a.Order,
	}
	if err := m.opts.Albums.CreateAlbum(ctx, native); err != nil {
		fmt.Fprintf(m.out, "\nalbum %q: %v\n", a.Title, err)
		summary.Failed++
		return
	}
	uidMap[a.UID] = native.UID
	summary.Created++
}

// backfillAlbumExtras fills location/category/notes/filter/order on an
// existing destination row when the destination value is still the
// column zero value and the PhotoPrism source has a non-default value.
// The spec only asks for the new columns to be backfilled, so user-
// editable columns (title/description/favorite/private) are left alone.
func (m *migrator) backfillAlbumExtras(
	ctx context.Context, a *ppAlbum, existing *database.Album,
) {
	if !mergeAlbumExtras(existing, a) {
		return
	}
	if err := m.opts.Albums.UpdateAlbum(ctx, existing); err != nil {
		fmt.Fprintf(m.out, "\nalbum %q backfill: %v\n", a.Title, err)
	}
}

// mergeAlbumExtras copies each PhotoPrism extra column into the
// destination only when the destination is still the column zero value.
// Returns true when at least one field was updated. Split out from
// backfillAlbumExtras to keep the cyclomatic-complexity per function
// inside the linter's budget.
func mergeAlbumExtras(existing *database.Album, a *ppAlbum) bool {
	pairs := []struct {
		dst *string
		src string
	}{
		{&existing.Location, a.Location},
		{&existing.Category, a.Category},
		{&existing.Notes, a.Notes},
		{&existing.Filter, a.Filter},
		{&existing.Order, a.Order},
	}
	changed := false
	for _, p := range pairs {
		if *p.dst == "" && p.src != "" {
			*p.dst = p.src
			changed = true
		}
	}
	return changed
}

// linkAlbumPhotos groups memberships by native album, then calls
// AlbumWriter.AddPhotos once per album (it handles its own dedup).
func (m *migrator) linkAlbumPhotos(
	ctx context.Context, memberships []ppAlbumPhoto, uidMap map[string]string,
) error {
	byAlbum := make(map[string][]string, len(uidMap))
	for _, link := range memberships {
		nativeAlbum, ok := uidMap[link.AlbumUID]
		if !ok {
			continue
		}
		nativePhoto, ok := m.photoMap[link.PhotoUID]
		if !ok {
			continue
		}
		byAlbum[nativeAlbum] = append(byAlbum[nativeAlbum], nativePhoto)
	}

	linkSummary := StageSummary{Stage: "album_photos", Read: len(memberships)}
	linkBar := newStageBar(len(byAlbum), "albums-link")
	defer finishBar(linkBar)
	for albumUID, photoUIDs := range byAlbum {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(linkSummary)
			return fmt.Errorf("album_photos canceled: %w", err)
		}
		_ = linkBar.Add(1)
		m.addOneAlbumLink(ctx, albumUID, photoUIDs, &linkSummary)
	}
	m.report.AppendStage(linkSummary)
	return nil
}

// addOneAlbumLink writes one album's photo memberships and updates the
// summary counters in place. AddPhotos uses ON CONFLICT DO NOTHING, so
// we look up existing memberships first to keep the create/skip counts
// honest on re-run.
func (m *migrator) addOneAlbumLink(
	ctx context.Context, albumUID string, photoUIDs []string, summary *StageSummary,
) {
	if m.opts.DryRun {
		summary.Created += len(photoUIDs)
		return
	}
	existing, err := m.opts.Albums.ListAlbumPhotoUIDs(ctx, albumUID)
	if err != nil {
		fmt.Fprintf(m.out, "\nalbum %s list members: %v\n", albumUID, err)
		summary.Failed += len(photoUIDs)
		return
	}
	have := make(map[string]struct{}, len(existing))
	for _, u := range existing {
		have[u] = struct{}{}
	}
	var toAdd []string
	for _, uid := range photoUIDs {
		if _, ok := have[uid]; ok {
			summary.Skipped++
			continue
		}
		toAdd = append(toAdd, uid)
	}
	if len(toAdd) == 0 {
		return
	}
	if err := m.opts.Albums.AddPhotos(ctx, albumUID, toAdd); err != nil {
		fmt.Fprintf(m.out, "\nalbum %s add %d photos: %v\n",
			albumUID, len(toAdd), err)
		summary.Failed += len(toAdd)
		return
	}
	summary.Created += len(toAdd)
}

// normaliseAlbumType folds PhotoPrism's "album/folder/moment/state/month"
// onto the native CHECK constraint. Anything unknown becomes "album"
// (PhotoPrism uses extra types like "default" / "manual" that we don't
// need to keep distinct).
func normaliseAlbumType(t string) string {
	switch t {
	case "album", "folder", "moment", "state", "month":
		return t
	default:
		return "album"
	}
}

// readPPAlbums loads non-deleted PhotoPrism albums. album_location /
// album_category / album_notes / album_filter / album_order are pulled
// into the projection so the migrator can populate the native columns
// added by migration 037 (task 332a727c). Smart albums (album_type !=
// 'album') keep their album_filter so a future smart-album feature can
// consume the DSL.
func (m *migrator) readPPAlbums(ctx context.Context) ([]ppAlbum, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx, `
		SELECT album_uid, COALESCE(album_slug, ''), COALESCE(album_title, ''),
		       COALESCE(album_description, ''), COALESCE(album_type, ''),
		       COALESCE(album_favorite, 0), COALESCE(album_private, 0),
		       COALESCE(album_location, ''),
		       COALESCE(album_category, ''),
		       COALESCE(album_notes, ''),
		       COALESCE(album_filter, ''),
		       COALESCE(album_order, '')
		FROM albums
		WHERE deleted_at IS NULL AND COALESCE(album_title, '') <> ''
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query albums: %w", err)
	}
	defer rows.Close()
	var out []ppAlbum
	for rows.Next() {
		var (
			a                          ppAlbum
			slugRaw, tRaw              []byte
			fav, priv                  int
			location, category         []byte
			notes, filterRaw, orderRaw []byte
		)
		if err := rows.Scan(
			&a.UID, &slugRaw, &a.Title, &a.Description, &tRaw, &fav, &priv,
			&location, &category, &notes, &filterRaw, &orderRaw,
		); err != nil {
			return nil, fmt.Errorf("scan album: %w", err)
		}
		a.Slug = string(slugRaw)
		a.Type = string(tRaw)
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
		return nil, fmt.Errorf("iterate albums: %w", err)
	}
	return out, nil
}

// readPPAlbumPhotos loads the photo↔album membership rows. PhotoPrism
// uses (album_uid, photo_uid) directly here so no JOIN is needed.
func (m *migrator) readPPAlbumPhotos(ctx context.Context) ([]ppAlbumPhoto, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx, `
		SELECT album_uid, photo_uid
		FROM photos_albums
		WHERE COALESCE(hidden, 0) = 0 AND COALESCE(missing, 0) = 0`)
	if err != nil {
		return nil, fmt.Errorf("query photos_albums: %w", err)
	}
	defer rows.Close()
	var out []ppAlbumPhoto
	for rows.Next() {
		var ap ppAlbumPhoto
		if err := rows.Scan(&ap.AlbumUID, &ap.PhotoUID); err != nil {
			return nil, fmt.Errorf("scan photos_albums: %w", err)
		}
		out = append(out, ap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos_albums: %w", err)
	}
	return out, nil
}
