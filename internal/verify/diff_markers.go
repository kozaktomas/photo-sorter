package verify

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppMarkerFull is the rich PhotoPrism marker projection used by the
// field-diff pass. Score / invalid / reviewed / subject_uid are pulled
// in on top of the geometry the structural diff already consumes.
type ppMarkerFull struct {
	MarkerUID  string
	FileUID    string
	SubjectUID string
	Type       string
	X, Y, W, H float64
	Score      int
	Invalid    bool
	Reviewed   bool
}

// diffMarkerFields compares score / invalid / reviewed / subject
// linkage for matched marker pairs. Pairing reuses the existing
// IoU-pick-closest strategy (same as the structural geometry diff) so a
// single PhotoPrism marker is only ever paired with one native marker
// and vice versa.
func (v *verifier) diffMarkerFields(ctx context.Context) error {
	ppMarkers, err := v.readPPMarkersFull(ctx)
	if err != nil {
		return fmt.Errorf("read pp markers full: %w", err)
	}
	subjMap, err := v.buildPPSubjectMap(ctx)
	if err != nil {
		return fmt.Errorf("build pp subject map: %w", err)
	}

	// Group PhotoPrism markers by the native photo they map to via
	// fileMap. Markers whose file has no native counterpart are skipped
	// (the photos section already reported the missing row).
	byPhoto := make(map[string][]ppMarkerFull)
	for _, mk := range ppMarkers {
		nativePhoto, ok := v.fileMap[mk.FileUID]
		if !ok {
			continue
		}
		byPhoto[nativePhoto] = append(byPhoto[nativePhoto], mk)
	}

	c := newFieldCollector("marker", &v.report.Markers.FieldDiffs)
	for photoUID, ppList := range byPhoto {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("markers field diff canceled: %w", err)
		}
		sorterMarkers, err := v.opts.Markers.ListMarkersForPhoto(ctx, photoUID)
		if err != nil {
			continue
		}
		v.compareMarkersOnPhoto(c, photoUID, ppList, sorterMarkers, subjMap)
	}
	return nil
}

// compareMarkersOnPhoto pairs PhotoPrism markers with the closest
// native marker on the same photo (by IoU) and emits field diffs for
// score / invalid / reviewed / subject_uid drift. Unmatched markers on
// either side are not emitted here — the structural geometry diff
// already lists them.
func (v *verifier) compareMarkersOnPhoto(
	c *fieldDiffCollector, photoUID string,
	ppList []ppMarkerFull, sorter []database.Marker, subjMap map[string]string,
) {
	if len(sorter) == 0 || len(ppList) == 0 {
		return
	}
	used := make([]bool, len(sorter))
	for _, pp := range ppList {
		idx := pickClosestMarkerForField(pp, sorter, used)
		if idx < 0 {
			continue
		}
		used[idx] = true
		sm := sorter[idx]
		key := markerKey(v.nativeHashByPhotoUID[photoUID], photoUID, sm)
		v.compareMarkerRow(c, key, pp, sm, subjMap)
	}
}

// compareMarkerRow emits score / invalid / reviewed / subject_uid
// diffs for one matched pair.
func (v *verifier) compareMarkerRow(
	c *fieldDiffCollector, key string, pp ppMarkerFull, sm database.Marker, subjMap map[string]string,
) {
	if !v.comparer.floatEq(float64(pp.Score), float64(sm.Score), MarkerScoreTolerance) {
		c.Push(key, "score", strconv.Itoa(pp.Score), strconv.Itoa(sm.Score))
	}
	if pp.Invalid != sm.Invalid {
		c.Push(key, "invalid", formatBool(pp.Invalid), formatBool(sm.Invalid))
	}
	if pp.Reviewed != sm.Reviewed {
		c.Push(key, "reviewed", formatBool(pp.Reviewed), formatBool(sm.Reviewed))
	}
	// Subject linkage: PhotoPrism subj_uid is preserved as the native
	// subjects.uid by the migrator, but in case an operator landed an
	// older buggy migrator we also resolve via subj_uid → subj_name →
	// native uid.
	srcSubject := pp.SubjectUID
	if srcSubject != "" {
		if mapped, ok := subjMap[srcSubject]; ok {
			srcSubject = mapped
		}
	}
	dstSubject := sm.SubjectUID
	if srcSubject != dstSubject {
		c.Push(key, "subject_uid", srcSubject, dstSubject)
	}
}

// markerKey returns a composite key identifying a marker for the diff
// report: (file_hash[:8], x.xxx, y.yyy, w.www, h.hhh). Falls back to
// the native photo UID when the hash is unknown.
func markerKey(fileHash, photoUID string, sm database.Marker) string {
	id := photoKey(fileHash, photoUID)
	return fmt.Sprintf("%s @ (%.3f,%.3f,%.3f,%.3f)", id, sm.X, sm.Y, sm.W, sm.H)
}

// pickClosestMarkerForField mirrors the structural pickClosestMarker
// but lives in this file to keep the field-diff pass independent of
// the structural marker pass.
func pickClosestMarkerForField(pp ppMarkerFull, sorters []database.Marker, used []bool) int {
	bestIdx := -1
	bestIoU := -1.0
	for i, s := range sorters {
		if used[i] {
			continue
		}
		iou := bboxIoU(pp.X, pp.Y, pp.W, pp.H, s.X, s.Y, s.W, s.H)
		if iou > bestIoU {
			bestIoU = iou
			bestIdx = i
		}
	}
	return bestIdx
}

// readPPMarkersFull loads the rich PhotoPrism marker projection.
func (v *verifier) readPPMarkersFull(ctx context.Context) ([]ppMarkerFull, error) {
	const query = `
		SELECT marker_uid, COALESCE(file_uid, ''), COALESCE(subj_uid, ''),
		       COALESCE(marker_type, ''),
		       COALESCE(x, 0), COALESCE(y, 0),
		       COALESCE(w, 0), COALESCE(h, 0),
		       COALESCE(score, 0),
		       COALESCE(marker_invalid, 0), COALESCE(marker_review, 0)
		FROM markers
		WHERE file_uid IS NOT NULL AND file_uid <> ''`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pp markers full: %w", err)
	}
	defer rows.Close()
	var out []ppMarkerFull
	for rows.Next() {
		var (
			mk                            ppMarkerFull
			x, y, w, h                    sql.NullFloat64
			invalid, reviewed             int
			markerUID, fileUID, subjectID []byte
			typeRaw                       []byte
		)
		if err := rows.Scan(
			&markerUID, &fileUID, &subjectID,
			&typeRaw,
			&x, &y, &w, &h,
			&mk.Score, &invalid, &reviewed,
		); err != nil {
			return nil, fmt.Errorf("scan pp marker full: %w", err)
		}
		mk.MarkerUID = string(markerUID)
		mk.FileUID = string(fileUID)
		mk.SubjectUID = string(subjectID)
		mk.Type = string(typeRaw)
		if x.Valid {
			mk.X = x.Float64
		}
		if y.Valid {
			mk.Y = y.Float64
		}
		if w.Valid {
			mk.W = w.Float64
		}
		if h.Valid {
			mk.H = h.Float64
		}
		mk.Invalid = invalid != 0
		mk.Reviewed = reviewed != 0
		out = append(out, mk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp markers full: %w", err)
	}
	return out, nil
}

// buildPPSubjectMap resolves the (PhotoPrism subj_uid → native subject
// UID) map by walking subjects and looking up each by name. The
// migrator's UID-preservation invariant means subj_uid == native UID
// for fresh migrations; we still build the map so legacy databases
// (pre-UID-preservation) compare cleanly.
func (v *verifier) buildPPSubjectMap(ctx context.Context) (map[string]string, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx,
		`SELECT subj_uid, subj_name FROM subjects
		 WHERE deleted_at IS NULL AND subj_name <> ''`)
	if err != nil {
		return nil, fmt.Errorf("query pp subjects (for marker diff): %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var uid, name string
		if err := rows.Scan(&uid, &name); err != nil {
			return nil, fmt.Errorf("scan pp subject (for marker diff): %w", err)
		}
		native, err := v.opts.Subjects.GetSubjectByName(ctx, name)
		if err != nil || native == nil {
			out[uid] = ""
			continue
		}
		out[uid] = native.UID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp subjects (for marker diff): %w", err)
	}
	return out, nil
}
