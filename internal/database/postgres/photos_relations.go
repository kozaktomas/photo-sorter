package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/lib/pq"
)

// LoadPhotoRelations fetches the requested relations for a whole page of
// photos and returns them keyed by photo UID.
//
// Every relation is one query over `photo_uid = ANY($1)`, not one query per
// photo. That matters: the single-photo readers that already exist
// (ListLabelsForPhoto, ListAlbumsForPhoto, ListMarkersForPhoto,
// ListPhotoFiles) would issue up to 4 × 500 = 2000 round-trips for one
// maximally-expanded page — the export would spend all its time in latency.
//
// Relations the caller did not ask for are left nil, which the wire layer
// renders as an absent field rather than an empty list. Photos with no rows
// for a requested relation get an empty (non-nil) slice, so `"labels": []`
// means "asked, and there are none" rather than "not asked".
func (r *PhotoRepository) LoadPhotoRelations(
	ctx context.Context, photoUIDs []string, include database.RelationSet,
) (map[string]*database.PhotoRelations, error) {
	out := make(map[string]*database.PhotoRelations, len(photoUIDs))
	for _, uid := range photoUIDs {
		out[uid] = &database.PhotoRelations{}
	}
	if len(photoUIDs) == 0 || include.Empty() {
		return out, nil
	}
	seedRequestedRelations(out, include)

	loaders := []struct {
		want bool
		load func(context.Context, []string, map[string]*database.PhotoRelations) error
	}{
		{include.Labels, r.loadLabelRelations},
		{include.Albums, r.loadAlbumRelations},
		{include.Markers, r.loadMarkerRelations},
		{include.Files, r.loadFileRelations},
	}
	for _, l := range loaders {
		if !l.want {
			continue
		}
		if err := l.load(ctx, photoUIDs, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// seedRequestedRelations pre-fills every requested relation with an empty
// non-nil slice.
//
// That is what lets the wire layer distinguish "you asked for labels and this
// photo has none" (`"labels": []`) from "you did not ask for labels" (field
// absent). An importer reading the former can safely clear its local labels;
// reading the latter it must not.
func seedRequestedRelations(
	out map[string]*database.PhotoRelations, include database.RelationSet,
) {
	for _, rel := range out {
		if include.Labels {
			rel.Labels = []database.PhotoLabelRelation{}
		}
		if include.Albums {
			rel.Albums = []database.PhotoAlbumRelation{}
		}
		if include.Markers {
			rel.Markers = []database.PhotoMarkerRelation{}
		}
		if include.Files {
			rel.Files = []database.PhotoFile{}
		}
	}
}

// loadLabelRelations attaches each photo's labels, carrying the provenance
// columns (source, uncertainty) off the photo_labels join row.
func (r *PhotoRepository) loadLabelRelations(
	ctx context.Context, photoUIDs []string, out map[string]*database.PhotoRelations,
) error {
	rows, err := r.pool.Query(ctx,
		`SELECT pl.photo_uid, l.uid, l.name, pl.source, pl.uncertainty
		 FROM photo_labels pl
		 JOIN labels l ON l.uid = pl.label_uid
		 WHERE pl.photo_uid = ANY($1)
		 ORDER BY pl.photo_uid, l.name`, pq.Array(photoUIDs))
	if err != nil {
		return fmt.Errorf("load photo labels: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var photoUID string
		var rel database.PhotoLabelRelation
		if err := rows.Scan(
			&photoUID, &rel.UID, &rel.Name, &rel.Source, &rel.Uncertainty,
		); err != nil {
			return fmt.Errorf("scan photo label: %w", err)
		}
		if target, ok := out[photoUID]; ok {
			target.Labels = append(target.Labels, rel)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate photo labels: %w", err)
	}
	return nil
}

// loadAlbumRelations attaches each photo's album memberships (uid + title).
func (r *PhotoRepository) loadAlbumRelations(
	ctx context.Context, photoUIDs []string, out map[string]*database.PhotoRelations,
) error {
	rows, err := r.pool.Query(ctx,
		`SELECT ap.photo_uid, a.uid, a.title
		 FROM album_photos ap
		 JOIN albums a ON a.uid = ap.album_uid
		 WHERE ap.photo_uid = ANY($1)
		 ORDER BY ap.photo_uid, a.title`, pq.Array(photoUIDs))
	if err != nil {
		return fmt.Errorf("load photo albums: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var photoUID string
		var rel database.PhotoAlbumRelation
		if err := rows.Scan(&photoUID, &rel.UID, &rel.Title); err != nil {
			return fmt.Errorf("scan photo album: %w", err)
		}
		if target, ok := out[photoUID]; ok {
			target.Albums = append(target.Albums, rel)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate photo albums: %w", err)
	}
	return nil
}

// loadMarkerRelations attaches each photo's markers, including the
// subject_uid that the /photos/{uid}/faces view omits. subject_uid is
// nullable (an unassigned face marker), so it scans through sql.NullString.
func (r *PhotoRepository) loadMarkerRelations(
	ctx context.Context, photoUIDs []string, out map[string]*database.PhotoRelations,
) error {
	rows, err := r.pool.Query(ctx,
		`SELECT photo_uid, uid, subject_uid, type, x, y, w, h, score, invalid, reviewed
		 FROM markers
		 WHERE photo_uid = ANY($1)
		 ORDER BY photo_uid, uid`, pq.Array(photoUIDs))
	if err != nil {
		return fmt.Errorf("load photo markers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var photoUID string
		var subjectUID sql.NullString
		var rel database.PhotoMarkerRelation
		if err := rows.Scan(
			&photoUID, &rel.UID, &subjectUID, &rel.Type,
			&rel.X, &rel.Y, &rel.W, &rel.H,
			&rel.Score, &rel.Invalid, &rel.Reviewed,
		); err != nil {
			return fmt.Errorf("scan photo marker: %w", err)
		}
		rel.SubjectUID = subjectUID.String
		if target, ok := out[photoUID]; ok {
			target.Markers = append(target.Markers, rel)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate photo markers: %w", err)
	}
	return nil
}

// loadFileRelations attaches each photo's physical file stack (original plus
// any RAW/JPEG sidecars and edited variants), primary file first.
func (r *PhotoRepository) loadFileRelations(
	ctx context.Context, photoUIDs []string, out map[string]*database.PhotoRelations,
) error {
	rows, err := r.pool.Query(ctx,
		`SELECT `+photoFileColumns+` FROM photo_files
		 WHERE photo_uid = ANY($1)
		 ORDER BY photo_uid, is_primary DESC, id`, pq.Array(photoUIDs))
	if err != nil {
		return fmt.Errorf("load photo files: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var f database.PhotoFile
		if err := rows.Scan(
			&f.ID, &f.PhotoUID, &f.FilePath, &f.FileHash, &f.FileSize, &f.FileMime,
			&f.IsPrimary, &f.Role, &f.CreatedAt,
		); err != nil {
			return fmt.Errorf("scan photo file: %w", err)
		}
		if target, ok := out[f.PhotoUID]; ok {
			target.Files = append(target.Files, f)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate photo files: %w", err)
	}
	return nil
}

// Verify interface compliance.
var _ database.PhotoRelationReader = (*PhotoRepository)(nil)
