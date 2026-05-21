package migrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppMarker is the projection of a PhotoPrism marker row.
type ppMarker struct {
	MarkerUID  string
	SubjectUID string
	FileUID    string
	X, Y, W, H float64
	Score      int
	Invalid    bool
	Reviewed   bool
	Type       string
}

// stageMarkers imports every PhotoPrism marker as a native marker. The
// marker→photo bridge runs through PhotoPrism's `file_uid`; the
// previously-computed fileMap turns that into the native photo UID. When
// a marker carries a subject_uid we look it up by name (subjects were
// already migrated) and assign it on insert.
//
// The PhotoPrism marker_uid is preserved as the native markers.uid so
// faces.marker_uid (which caches the PhotoPrism UID) keeps pointing at
// the right row. Idempotency comes from the photoMap — a re-imported
// photo skips the pre-existing UID and so do its markers.
func (m *migrator) stageMarkers(ctx context.Context) error {
	markers, err := m.readPPMarkers(ctx)
	if err != nil {
		return fmt.Errorf("read markers: %w", err)
	}
	// PhotoPrism subj_uid → native subject UID. We resolve via
	// SubjectReader.GetSubject inside the loop and cache here so
	// repeated subjects don't hit Postgres on every marker.
	subjMap := make(map[string]string)

	summary := StageSummary{Stage: StageMarkers, Read: len(markers)}
	bar := newStageBar(len(markers), "markers")
	defer finishBar(bar)

	for i := range markers {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(summary)
			return fmt.Errorf("markers canceled: %w", err)
		}
		_ = bar.Add(1)
		mk := &markers[i]
		nativePhoto, ok := m.fileMap[mk.FileUID]
		if !ok {
			summary.Skipped++
			continue
		}
		// Markers carry no natural unique key the migrator can rely on,
		// so they are only inserted for photos this run actually
		// created. A photo that was already in the destination at the
		// start of the run is presumed to already carry its markers.
		if !m.opts.DryRun && !m.newPhotos[nativePhoto] {
			summary.Skipped++
			continue
		}
		if m.opts.DryRun {
			summary.Created++
			continue
		}
		nativeSubject := m.resolveSubjectUID(ctx, subjMap, mk.SubjectUID)

		marker := &database.Marker{
			// Preserve the PhotoPrism marker_uid so faces.marker_uid
			// (which caches the PhotoPrism UID) keeps pointing at the
			// right row after the migration.
			UID:        mk.MarkerUID,
			PhotoUID:   nativePhoto,
			SubjectUID: nativeSubject,
			Type:       markerType(mk.Type),
			X:          mk.X,
			Y:          mk.Y,
			W:          mk.W,
			H:          mk.H,
			Score:      mk.Score,
			Invalid:    mk.Invalid,
			Reviewed:   mk.Reviewed,
		}
		if err := m.opts.Markers.CreateMarker(ctx, marker); err != nil {
			fmt.Fprintf(m.out, "\nmarker %s: %v\n", mk.MarkerUID, err)
			summary.Failed++
			continue
		}
		summary.Created++
	}
	m.report.AppendStage(summary)
	return nil
}

// resolveSubjectUID translates a PhotoPrism subj_uid into a native
// subject UID. The subjects stage already inserted every subject by name,
// so we look the PhotoPrism row up by name and consult the native store.
// Cache misses are silently treated as "unassigned" so an orphan marker
// still lands without a subject.
func (m *migrator) resolveSubjectUID(
	ctx context.Context, cache map[string]string, ppSubjUID string,
) string {
	if ppSubjUID == "" {
		return ""
	}
	if uid, ok := cache[ppSubjUID]; ok {
		return uid
	}
	var name string
	err := m.opts.MariaDB.QueryRowContext(ctx,
		`SELECT subj_name FROM subjects WHERE subj_uid = ?`, ppSubjUID).Scan(&name)
	if err != nil {
		cache[ppSubjUID] = ""
		return ""
	}
	native, err := m.opts.Subjects.GetSubjectByName(ctx, name)
	if err != nil || native == nil {
		cache[ppSubjUID] = ""
		return ""
	}
	cache[ppSubjUID] = native.UID
	return native.UID
}

// markerType normalises PhotoPrism's marker_type onto the native CHECK
// constraint. "face" and "label" pass through; everything else becomes
// "face" (the dominant case).
func markerType(t string) string {
	switch t {
	case "face", "label":
		return t
	default:
		return "face"
	}
}

// readPPMarkers loads every marker row attached to a non-missing file.
// Markers without a file_uid are skipped (they are detached/legacy rows
// PhotoPrism never cleaned up).
func (m *migrator) readPPMarkers(ctx context.Context) ([]ppMarker, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx, `
		SELECT marker_uid, COALESCE(subj_uid, ''), COALESCE(file_uid, ''),
		       COALESCE(x, 0), COALESCE(y, 0),
		       COALESCE(w, 0), COALESCE(h, 0),
		       COALESCE(score, 0),
		       COALESCE(marker_invalid, 0), COALESCE(marker_review, 0),
		       COALESCE(marker_type, '')
		FROM markers
		WHERE file_uid IS NOT NULL AND file_uid <> ''`)
	if err != nil {
		return nil, fmt.Errorf("query markers: %w", err)
	}
	defer rows.Close()
	var out []ppMarker
	for rows.Next() {
		var (
			mk                            ppMarker
			invalid, reviewed             int
			x, y, w, h                    sql.NullFloat64
			typeRaw                       []byte
			subjUidRaw, fileUidRaw, mkUid []byte
		)
		if err := rows.Scan(
			&mkUid, &subjUidRaw, &fileUidRaw,
			&x, &y, &w, &h,
			&mk.Score, &invalid, &reviewed, &typeRaw,
		); err != nil {
			return nil, fmt.Errorf("scan marker: %w", err)
		}
		mk.MarkerUID = string(mkUid)
		mk.SubjectUID = string(subjUidRaw)
		mk.FileUID = string(fileUidRaw)
		mk.X = nullFloat(x)
		mk.Y = nullFloat(y)
		mk.W = nullFloat(w)
		mk.H = nullFloat(h)
		mk.Invalid = invalid != 0
		mk.Reviewed = reviewed != 0
		mk.Type = string(typeRaw)
		out = append(out, mk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate markers: %w", err)
	}
	return out, nil
}

func nullFloat(v sql.NullFloat64) float64 {
	if v.Valid {
		return v.Float64
	}
	return 0
}
