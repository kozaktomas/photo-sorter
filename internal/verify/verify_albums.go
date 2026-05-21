package verify

import (
	"context"
	"fmt"
	"sort"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppAlbumRow is the minimal projection of a PhotoPrism album row the
// verifier needs.
type ppAlbumRow struct {
	UID   string
	Slug  string
	Title string
}

// runAlbums compares slug/title pairs and per-album photo memberships.
// PhotoPrism is iterated first (it is the source of truth before the
// migration); each row is looked up by slug in the sorter and any
// title mismatch is reported. The sorter is then walked to find albums
// with no PhotoPrism counterpart.
func (v *verifier) runAlbums(ctx context.Context) error {
	ppAlbums, err := v.readPPAlbums(ctx)
	if err != nil {
		return fmt.Errorf("read pp albums: %w", err)
	}
	v.report.Albums.PPCount = len(ppAlbums)

	ppMembers, err := v.readPPAlbumPhotos(ctx)
	if err != nil {
		return fmt.Errorf("read pp album photos: %w", err)
	}

	if err := v.compareAlbumsBySlug(ctx, ppAlbums, ppMembers); err != nil {
		return err
	}
	return v.findOrphanAlbums(ctx, ppAlbums)
}

// compareAlbumsBySlug iterates PhotoPrism albums, resolves the matching
// sorter album by slug, records slug/title mismatches, missing rows, and
// per-album photo set differences.
func (v *verifier) compareAlbumsBySlug(
	ctx context.Context, ppAlbums []ppAlbumRow, ppMembers map[string][]string,
) error {
	var (
		titleMiss  []AlbumDiff
		missing    []string
		photoDiffs []AlbumPhoto
	)
	for _, a := range ppAlbums {
		if a.Slug == "" {
			continue
		}
		native, err := v.opts.Albums.GetAlbumBySlug(ctx, a.Slug)
		if err != nil || native == nil {
			missing = append(missing, a.Slug)
			continue
		}
		if native.Title != a.Title {
			titleMiss = append(titleMiss, AlbumDiff{
				Slug:       a.Slug,
				PPTitle:    a.Title,
				NativeUID:  native.UID,
				NativeName: native.Title,
			})
		}
		diff, ok := v.diffAlbumPhotos(ctx, a, native.UID, ppMembers[a.UID])
		if ok {
			photoDiffs = append(photoDiffs, diff)
		}
	}
	sort.Strings(missing)
	v.report.Albums.SlugTitleMismatch = titleMiss
	v.report.Albums.MissingInSorter = truncate(missing)
	v.report.Albums.PhotoDiffs = photoDiffs
	return nil
}

// diffAlbumPhotos compares the photo membership of one album between
// PhotoPrism and the sorter. ppPhotos is the list of PhotoPrism photo
// UIDs in the album; these are translated through v.photoMap to native
// UIDs. The function returns ok=true only when there is an actual diff
// — equal sides produce ok=false so the caller does not emit a noop
// entry.
func (v *verifier) diffAlbumPhotos(
	ctx context.Context, a ppAlbumRow, nativeAlbumUID string, ppPhotos []string,
) (AlbumPhoto, bool) {
	ppNativeSet := translateAlbumMembers(v.photoMap, ppPhotos)
	sorterUIDs, err := v.opts.Albums.ListAlbumPhotoUIDs(ctx, nativeAlbumUID)
	if err != nil {
		return AlbumPhoto{}, false
	}
	sorterSet := stringSliceToSet(sorterUIDs)
	missing, orphan := symmetricStringDiff(ppNativeSet, sorterSet, sorterUIDs)
	if len(missing) == 0 && len(orphan) == 0 && len(ppNativeSet) == len(sorterSet) {
		return AlbumPhoto{}, false
	}
	sort.Strings(missing)
	sort.Strings(orphan)
	return AlbumPhoto{
		Slug:            a.Slug,
		PPCount:         len(ppNativeSet),
		SorterCount:     len(sorterSet),
		MissingInSorter: truncate(missing),
		OrphanInSorter:  truncate(orphan),
	}, true
}

// translateAlbumMembers walks the PhotoPrism photo UIDs and produces the
// set of native photo UIDs that the sorter should hold. PhotoPrism rows
// that never made it through the migration are silently dropped — the
// photo-level missing report already calls them out.
func translateAlbumMembers(photoMap map[string]string, ppPhotos []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ppPhotos))
	for _, ppPhoto := range ppPhotos {
		native, ok := photoMap[ppPhoto]
		if !ok {
			continue
		}
		out[native] = struct{}{}
	}
	return out
}

// stringSliceToSet returns a set-style map for the given slice.
func stringSliceToSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}

// symmetricStringDiff returns (missing, orphan) where missing is every
// element of pp not in sorterSet, orphan is every element of the
// sorter-slice (used to preserve sort order) not in pp.
func symmetricStringDiff(
	pp, sorterSet map[string]struct{}, sorterOrder []string,
) ([]string, []string) {
	var missing, orphan []string
	for uid := range pp {
		if _, ok := sorterSet[uid]; !ok {
			missing = append(missing, uid)
		}
	}
	for _, uid := range sorterOrder {
		if _, ok := pp[uid]; !ok {
			orphan = append(orphan, uid)
		}
	}
	return missing, orphan
}

// findOrphanAlbums walks the sorter's album list and flags any slug that
// is not present in PhotoPrism.
func (v *verifier) findOrphanAlbums(ctx context.Context, ppAlbums []ppAlbumRow) error {
	ppSlugs := make(map[string]struct{}, len(ppAlbums))
	for _, a := range ppAlbums {
		if a.Slug != "" {
			ppSlugs[a.Slug] = struct{}{}
		}
	}

	var orphans []string
	total := 0
	offset := 0
	const pageSize = 200
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("orphan album walk canceled: %w", err)
		}
		page, err := v.opts.Albums.ListAlbums(ctx, database.AlbumQuery{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("list sorter albums: %w", err)
		}
		if len(page) == 0 {
			break
		}
		for _, a := range page {
			total++
			if _, ok := ppSlugs[a.Slug]; !ok {
				orphans = append(orphans, a.Slug)
			}
		}
		offset += len(page)
	}
	v.report.Albums.SorterCount = total
	sort.Strings(orphans)
	v.report.Albums.OrphanInSorter = truncate(orphans)
	return nil
}

// readPPAlbums loads the slug/title/uid of every non-deleted PhotoPrism
// album. Empty-titled rows are skipped — PhotoPrism creates them as
// placeholders for ad-hoc folders.
func (v *verifier) readPPAlbums(ctx context.Context) ([]ppAlbumRow, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx, `
		SELECT album_uid, COALESCE(album_slug, ''), COALESCE(album_title, '')
		FROM albums
		WHERE deleted_at IS NULL AND COALESCE(album_title, '') <> ''
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query pp albums: %w", err)
	}
	defer rows.Close()
	var out []ppAlbumRow
	for rows.Next() {
		var (
			a               ppAlbumRow
			slugRaw, uidRaw []byte
		)
		if err := rows.Scan(&uidRaw, &slugRaw, &a.Title); err != nil {
			return nil, fmt.Errorf("scan pp album: %w", err)
		}
		a.UID = string(uidRaw)
		a.Slug = string(slugRaw)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp albums: %w", err)
	}
	return out, nil
}

// readPPAlbumPhotos returns the photo memberships of every PhotoPrism
// album, keyed by album_uid. Hidden / missing memberships are excluded
// to match the migrator's behaviour.
func (v *verifier) readPPAlbumPhotos(ctx context.Context) (map[string][]string, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx, `
		SELECT album_uid, photo_uid FROM photos_albums
		WHERE COALESCE(hidden, 0) = 0 AND COALESCE(missing, 0) = 0`)
	if err != nil {
		return nil, fmt.Errorf("query pp photos_albums: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var albumUID, photoUID string
		if err := rows.Scan(&albumUID, &photoUID); err != nil {
			return nil, fmt.Errorf("scan pp photos_albums: %w", err)
		}
		out[albumUID] = append(out[albumUID], photoUID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp photos_albums: %w", err)
	}
	return out, nil
}
