package verify

import (
	"context"
	"fmt"
	"sort"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// photoFilterByLabel builds a PhotoFilter that selects every photo
// carrying the given label. The caller iterates with archived=nil
// (exclude archived) and archived=true (only archived) so the result
// covers the full set — photo-sorter's filter has no "include archived
// too" mode and we want to compare against PhotoPrism, which has no
// archived concept on the photo row itself.
func photoFilterByLabel(labelUID string, archived *bool, limit, offset int) database.PhotoFilter {
	return database.PhotoFilter{
		LabelUIDs: []string{labelUID},
		Archived:  archived,
		Limit:     limit,
		Offset:    offset,
	}
}

// diffJunctionTables widens the album_photos and photo_labels pair
// counts into a per-pair diff: for every membership in PhotoPrism that
// has no native counterpart (or vice versa) emit one
// MembershipPairDiff with the container slug + the photo file_hash.
//
// Photos that did not migrate at all are NOT reported here — they are
// already in the photos section. Containers (albums / labels) that did
// not migrate are also not reported here — they are already in their
// section. The junction diff only fires when both the photo and the
// container exist on both sides but their link is asymmetric.
func (v *verifier) diffJunctionTables(ctx context.Context) error {
	if err := v.diffAlbumMembership(ctx); err != nil {
		return fmt.Errorf("album_photos: %w", err)
	}
	if err := v.diffLabelMembership(ctx); err != nil {
		return fmt.Errorf("photos_labels: %w", err)
	}
	return nil
}

// diffAlbumMembership compares photos_albums (PhotoPrism) against
// album_photos (native) per album slug. For each album that exists on
// both sides we compute the symmetric difference of the photo set and
// emit one MembershipPairDiff per asymmetric photo.
func (v *verifier) diffAlbumMembership(ctx context.Context) error {
	ppMembers, _, err := v.readPPAlbumMembershipBySlug(ctx)
	if err != nil {
		return err
	}
	var diffs []MembershipPairDiff
	for slug, ppPhotos := range ppMembers {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("album membership diff canceled: %w", err)
		}
		native, err := v.opts.Albums.GetAlbumBySlug(ctx, slug)
		if err != nil || native == nil {
			continue
		}
		nativePhotos, err := v.opts.Albums.ListAlbumPhotoUIDs(ctx, native.UID)
		if err != nil {
			continue
		}
		diffs = appendMembershipDiff(diffs, slug, ppPhotos, nativePhotos, v.photoMap, v.nativeHashByPhotoUID)
	}
	sort.SliceStable(diffs, func(i, j int) bool {
		if diffs[i].ContainerSlug != diffs[j].ContainerSlug {
			return diffs[i].ContainerSlug < diffs[j].ContainerSlug
		}
		if diffs[i].Side != diffs[j].Side {
			return diffs[i].Side < diffs[j].Side
		}
		return diffs[i].PhotoFileHash < diffs[j].PhotoFileHash
	})
	if len(diffs) > MaxFieldDiffsPerField {
		diffs = diffs[:MaxFieldDiffsPerField]
	}
	v.report.Albums.MembershipDiffs = diffs
	return nil
}

// diffLabelMembership compares photos_labels (PhotoPrism) against
// photo_labels (native) per label slug.
func (v *verifier) diffLabelMembership(ctx context.Context) error {
	ppMembers, err := v.readPPLabelMembershipBySlug(ctx)
	if err != nil {
		return err
	}
	var diffs []MembershipPairDiff
	for slug, ppPhotos := range ppMembers {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("label membership diff canceled: %w", err)
		}
		native, err := v.opts.Labels.GetLabelBySlug(ctx, slug)
		if err != nil || native == nil {
			continue
		}
		nativePhotos, err := v.listPhotosForLabel(ctx, native.UID)
		if err != nil {
			continue
		}
		diffs = appendMembershipDiff(diffs, slug, ppPhotos, nativePhotos, v.photoMap, v.nativeHashByPhotoUID)
	}
	sort.SliceStable(diffs, func(i, j int) bool {
		if diffs[i].ContainerSlug != diffs[j].ContainerSlug {
			return diffs[i].ContainerSlug < diffs[j].ContainerSlug
		}
		if diffs[i].Side != diffs[j].Side {
			return diffs[i].Side < diffs[j].Side
		}
		return diffs[i].PhotoFileHash < diffs[j].PhotoFileHash
	})
	if len(diffs) > MaxFieldDiffsPerField {
		diffs = diffs[:MaxFieldDiffsPerField]
	}
	v.report.Labels.MembershipDiffs = diffs
	return nil
}

// appendMembershipDiff computes the symmetric difference of
// translated(ppPhotos) and nativePhotos and appends one
// MembershipPairDiff per asymmetric photo. ppPhotos carries PhotoPrism
// photo UIDs which are translated through photoMap; photos missing
// from photoMap are skipped (they did not migrate, the photos section
// already reported them).
func appendMembershipDiff(
	out []MembershipPairDiff, slug string,
	ppPhotos, nativePhotos []string,
	photoMap, hashByUID map[string]string,
) []MembershipPairDiff {
	ppSet := make(map[string]struct{}, len(ppPhotos))
	for _, ppUID := range ppPhotos {
		nativeUID, ok := photoMap[ppUID]
		if !ok {
			continue
		}
		ppSet[nativeUID] = struct{}{}
	}
	nativeSet := make(map[string]struct{}, len(nativePhotos))
	for _, uid := range nativePhotos {
		nativeSet[uid] = struct{}{}
	}
	for uid := range ppSet {
		if _, ok := nativeSet[uid]; !ok {
			out = append(out, MembershipPairDiff{
				ContainerSlug: slug,
				PhotoFileHash: photoIdentifier(uid, hashByUID),
				Side:          "pp_only",
			})
		}
	}
	for uid := range nativeSet {
		if _, ok := ppSet[uid]; !ok {
			out = append(out, MembershipPairDiff{
				ContainerSlug: slug,
				PhotoFileHash: photoIdentifier(uid, hashByUID),
				Side:          "sorter_only",
			})
		}
	}
	return out
}

// photoIdentifier returns a stable, human-readable identifier for a
// native photo UID: the cached file_hash[:8] when available, else the
// UID itself.
func photoIdentifier(uid string, hashByUID map[string]string) string {
	if hash := hashByUID[uid]; hash != "" {
		if len(hash) >= 8 {
			return hash[:8]
		}
		return hash
	}
	return uid
}

// readPPAlbumMembershipBySlug returns a map slug → []photo_uid for
// every non-deleted PhotoPrism album. ppSlugs maps slug → album_uid so
// the diff loop can dereference the native row by slug only.
func (v *verifier) readPPAlbumMembershipBySlug(
	ctx context.Context,
) (map[string][]string, map[string]string, error) {
	const query = `
		SELECT a.album_slug, a.album_uid, pa.photo_uid
		FROM albums a
		JOIN photos_albums pa ON pa.album_uid = a.album_uid
		WHERE a.deleted_at IS NULL
		  AND COALESCE(a.album_slug, '') <> ''
		  AND COALESCE(pa.hidden, 0) = 0
		  AND COALESCE(pa.missing, 0) = 0`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("query pp album membership: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	slugMap := make(map[string]string)
	for rows.Next() {
		var slug, albumUID, photoUID []byte
		if err := rows.Scan(&slug, &albumUID, &photoUID); err != nil {
			return nil, nil, fmt.Errorf("scan pp album membership: %w", err)
		}
		s := string(slug)
		if s == "" {
			continue
		}
		out[s] = append(out[s], string(photoUID))
		slugMap[s] = string(albumUID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate pp album membership: %w", err)
	}
	return out, slugMap, nil
}

// readPPLabelMembershipBySlug returns a map slug → []photo_uid for
// every non-deleted PhotoPrism label.
func (v *verifier) readPPLabelMembershipBySlug(
	ctx context.Context,
) (map[string][]string, error) {
	const query = `
		SELECT l.label_slug, p.photo_uid
		FROM photos_labels pl
		JOIN labels l ON l.id = pl.label_id
		JOIN photos p ON p.id = pl.photo_id
		WHERE l.deleted_at IS NULL
		  AND COALESCE(l.label_priority, 0) >= 0
		  AND COALESCE(l.label_slug, '') <> ''
		  AND p.deleted_at IS NULL`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pp label membership: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var slug, photoUID []byte
		if err := rows.Scan(&slug, &photoUID); err != nil {
			return nil, fmt.Errorf("scan pp label membership: %w", err)
		}
		s := string(slug)
		if s == "" {
			continue
		}
		out[s] = append(out[s], string(photoUID))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp label membership: %w", err)
	}
	return out, nil
}

// listPhotosForLabel returns every native photo UID that carries the
// given label, including archived rows. PhotoFilter exposes archived
// state as one of three values (exclude / only / explicit-non), with
// no "include both" mode, so the verifier iterates non-archived then
// archived and unions them.
func (v *verifier) listPhotosForLabel(ctx context.Context, labelUID string) ([]string, error) {
	out := make([]string, 0)
	for _, archived := range []*bool{nil, new(bool)} {
		if archived != nil {
			*archived = true
		}
		if err := v.appendPhotosForLabel(ctx, labelUID, archived, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// appendPhotosForLabel pages through the photos that carry labelUID
// under the given archived filter, appending their UIDs to *out.
func (v *verifier) appendPhotosForLabel(
	ctx context.Context, labelUID string, archived *bool, out *[]string,
) error {
	const pageSize = 500
	offset := 0
	for {
		page, _, err := v.opts.Photos.ListPhotos(ctx, photoFilterByLabel(labelUID, archived, pageSize, offset))
		if err != nil {
			return fmt.Errorf("list photos for label %s: %w", labelUID, err)
		}
		if len(page) == 0 {
			return nil
		}
		for _, p := range page {
			*out = append(*out, p.UID)
		}
		offset += len(page)
	}
}
