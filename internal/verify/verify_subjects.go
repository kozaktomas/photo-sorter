package verify

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
)

// ppSubjectRow is the minimal projection of a PhotoPrism subject row.
type ppSubjectRow struct {
	UID  string
	Name string
}

// ppMarkerRow is the minimal projection of a PhotoPrism marker row used
// by the geometry diff pass.
type ppMarkerRow struct {
	MarkerUID  string
	FileUID    string
	SubjectUID string
	X, Y, W, H float64
}

// runSubjectsAndMarkers compares subjects by accent-insensitive
// lowercased name (matching the migrator's idempotency rule), then
// compares per-subject marker counts and detects per-marker geometry
// drift > GeometryTolerance.
func (v *verifier) runSubjectsAndMarkers(ctx context.Context) error {
	ppSubjects, err := v.readPPSubjects(ctx)
	if err != nil {
		return fmt.Errorf("read pp subjects: %w", err)
	}
	v.report.Subjects.PPCount = len(ppSubjects)

	subjectMap, missing := v.matchSubjectsByName(ctx, ppSubjects)
	v.report.Subjects.MissingInSorter = truncate(missing)

	if err := v.findOrphanSubjects(ctx, ppSubjects); err != nil {
		return err
	}

	ppMarkers, err := v.readPPMarkers(ctx)
	if err != nil {
		return fmt.Errorf("read pp markers: %w", err)
	}
	v.report.Markers.PPCount = len(ppMarkers)

	return v.diffMarkers(ctx, ppSubjects, ppMarkers, subjectMap)
}

// matchSubjectsByName builds the (pp subject_uid → native subject UID)
// map and the list of PhotoPrism subjects with no sorter counterpart.
// Names are normalised with facematch.NormalizePersonName so the
// comparison is accent-insensitive and case-insensitive.
func (v *verifier) matchSubjectsByName(
	ctx context.Context, ppSubjects []ppSubjectRow,
) (map[string]string, []string) {
	subjectMap := make(map[string]string, len(ppSubjects))
	var missing []string
	for _, s := range ppSubjects {
		native, err := v.opts.Subjects.GetSubjectByName(ctx, s.Name)
		if err != nil || native == nil {
			missing = append(missing, s.Name)
			continue
		}
		subjectMap[s.UID] = native.UID
	}
	sort.Strings(missing)
	return subjectMap, missing
}

// findOrphanSubjects walks the sorter's subject list and flags those
// whose normalised name has no PhotoPrism counterpart.
func (v *verifier) findOrphanSubjects(ctx context.Context, ppSubjects []ppSubjectRow) error {
	ppNames := make(map[string]struct{}, len(ppSubjects))
	for _, s := range ppSubjects {
		ppNames[facematch.NormalizePersonName(s.Name)] = struct{}{}
	}

	var orphans []string
	total := 0
	offset := 0
	const pageSize = 200
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("orphan subject walk canceled: %w", err)
		}
		page, err := v.opts.Subjects.ListSubjects(ctx, database.SubjectQuery{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("list sorter subjects: %w", err)
		}
		if len(page) == 0 {
			break
		}
		for _, s := range page {
			total++
			if _, ok := ppNames[facematch.NormalizePersonName(s.Name)]; !ok {
				orphans = append(orphans, s.Name)
			}
		}
		offset += len(page)
	}
	v.report.Subjects.SorterCount = total
	sort.Strings(orphans)
	v.report.Subjects.OrphanInSorter = truncate(orphans)
	return nil
}

// diffMarkers compares marker counts per subject and geometry per
// marker. The native side is enumerated by walking ListMarkersForPhoto
// for every native photo we matched, which keeps the implementation
// scoped to the photos the migration actually moved.
func (v *verifier) diffMarkers(
	ctx context.Context, ppSubjects []ppSubjectRow, ppMarkers []ppMarkerRow,
	subjectMap map[string]string,
) error {
	// Group PhotoPrism markers by (file_uid → native photo UID); also
	// count how many markers each native subject has on the PhotoPrism
	// side for the count diff.
	ppMarkersByNativePhoto := make(map[string][]ppMarkerRow)
	ppCountBySubject := make(map[string]int)
	for _, mk := range ppMarkers {
		nativePhoto, ok := v.fileMap[mk.FileUID]
		if !ok {
			continue
		}
		ppMarkersByNativePhoto[nativePhoto] = append(ppMarkersByNativePhoto[nativePhoto], mk)
		if mk.SubjectUID != "" {
			if nativeSubject, ok := subjectMap[mk.SubjectUID]; ok {
				ppCountBySubject[nativeSubject]++
			}
		}
	}

	geomDiffs, sorterTotal, sorterBySubject, err := v.compareMarkersPerPhoto(ctx, ppMarkersByNativePhoto)
	if err != nil {
		return err
	}
	v.report.Markers.SorterCount = sorterTotal
	v.report.Markers.GeometryDiffs = geomDiffs

	v.report.Markers.CountDiffs = buildMarkerCountDiffs(ppSubjects, subjectMap, ppCountBySubject, sorterBySubject)
	return nil
}

// buildMarkerCountDiffs produces one MarkerCountDiff per subject whose
// PhotoPrism marker count does not match its sorter count. Only
// subjects present on the PhotoPrism side are considered (orphan
// subjects already get a dedicated entry in SubjectReport).
func buildMarkerCountDiffs(
	ppSubjects []ppSubjectRow, subjectMap map[string]string,
	ppCount, sorterCount map[string]int,
) []MarkerCountDiff {
	var diffs []MarkerCountDiff
	for _, s := range ppSubjects {
		nativeUID, ok := subjectMap[s.UID]
		if !ok {
			continue
		}
		pp := ppCount[nativeUID]
		sorter := sorterCount[nativeUID]
		if pp != sorter {
			diffs = append(diffs, MarkerCountDiff{
				SubjectName: s.Name,
				PPCount:     pp,
				SorterCount: sorter,
			})
		}
	}
	return diffs
}

// compareMarkersPerPhoto walks the union of native photos that either
// PhotoPrism or the sorter has markers for. For each photo it lists the
// sorter markers, tallies them by subject, and pairs them against the
// PhotoPrism markers attached to the same photo (resolved via fileMap)
// to record geometry drift. Returns:
//
//   - the geometry diff slice (capped at MaxItemsPerCategory),
//   - the sorter's total marker count for the section header,
//   - per-subject sorter marker counts for buildMarkerCountDiffs.
func (v *verifier) compareMarkersPerPhoto(
	ctx context.Context, ppMarkersByNativePhoto map[string][]ppMarkerRow,
) ([]MarkerGeometryDiff, int, map[string]int, error) {
	var diffs []MarkerGeometryDiff
	sorterBySubject := make(map[string]int)
	sorterTotal := 0

	nativePhotos := collectNativePhotosForMarkers(v.photoMap, ppMarkersByNativePhoto)
	for _, nativePhoto := range nativePhotos {
		if err := ctx.Err(); err != nil {
			return diffs, sorterTotal, sorterBySubject, fmt.Errorf("marker diff canceled: %w", err)
		}
		sorterMarkers, err := v.opts.Markers.ListMarkersForPhoto(ctx, nativePhoto)
		if err != nil {
			return diffs, sorterTotal, sorterBySubject, fmt.Errorf("list sorter markers: %w", err)
		}
		sorterTotal += len(sorterMarkers)
		for _, m := range sorterMarkers {
			if m.SubjectUID != "" {
				sorterBySubject[m.SubjectUID]++
			}
		}
		if len(diffs) >= MaxItemsPerCategory {
			continue
		}
		diffs = appendGeometryDiffs(diffs, nativePhoto, ppMarkersByNativePhoto[nativePhoto], sorterMarkers)
	}
	if len(diffs) > MaxItemsPerCategory {
		diffs = diffs[:MaxItemsPerCategory]
	}
	return diffs, sorterTotal, sorterBySubject, nil
}

// collectNativePhotosForMarkers returns the deduplicated, sorted list of
// native photo UIDs we need to look up markers for: the union of every
// photo we successfully matched plus every photo PhotoPrism has a marker
// on. Sorting keeps the output of the verifier deterministic for tests.
func collectNativePhotosForMarkers(
	photoMap map[string]string, ppMarkers map[string][]ppMarkerRow,
) []string {
	set := make(map[string]struct{}, len(photoMap)+len(ppMarkers))
	for _, nativeUID := range photoMap {
		set[nativeUID] = struct{}{}
	}
	for nativeUID := range ppMarkers {
		set[nativeUID] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for uid := range set {
		out = append(out, uid)
	}
	sort.Strings(out)
	return out
}

// appendGeometryDiffs pairs each PhotoPrism marker to its highest-IoU
// sorter marker on the same photo and appends a diff entry whenever the
// x/y/w/h deviation exceeds GeometryTolerance on any axis.
func appendGeometryDiffs(
	diffs []MarkerGeometryDiff, nativePhoto string,
	ppList []ppMarkerRow, sorterMarkers []database.Marker,
) []MarkerGeometryDiff {
	if len(ppList) == 0 || len(sorterMarkers) == 0 {
		return diffs
	}
	used := make([]bool, len(sorterMarkers))
	for _, pp := range ppList {
		if len(diffs) >= MaxItemsPerCategory {
			break
		}
		idx := pickClosestMarker(pp, sorterMarkers, used)
		if idx < 0 {
			continue
		}
		used[idx] = true
		sm := sorterMarkers[idx]
		if geometryWithinTolerance(pp, sm) {
			continue
		}
		diffs = append(diffs, MarkerGeometryDiff{
			PhotoSorterUID: nativePhoto,
			PPMarkerUID:    pp.MarkerUID,
			PPX:            pp.X, PPY: pp.Y, PPW: pp.W, PPH: pp.H,
			SorterX: sm.X, SorterY: sm.Y, SorterW: sm.W, SorterH: sm.H,
		})
	}
	return diffs
}

// pickClosestMarker returns the index of the sorter marker with the
// highest IoU against the PhotoPrism marker. Markers already used in a
// prior pairing are skipped. -1 means no candidate remained.
func pickClosestMarker(pp ppMarkerRow, sorters []database.Marker, used []bool) int {
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

// geometryWithinTolerance reports whether every axis of the two bboxes
// is within GeometryTolerance of the other.
func geometryWithinTolerance(pp ppMarkerRow, s database.Marker) bool {
	if math.Abs(pp.X-s.X) > GeometryTolerance {
		return false
	}
	if math.Abs(pp.Y-s.Y) > GeometryTolerance {
		return false
	}
	if math.Abs(pp.W-s.W) > GeometryTolerance {
		return false
	}
	if math.Abs(pp.H-s.H) > GeometryTolerance {
		return false
	}
	return true
}

// bboxIoU computes the intersection-over-union of two top-left,
// width/height bounding boxes. Returns 0 for degenerate inputs (zero
// area) so callers do not have to special-case the empty marker.
func bboxIoU(ax, ay, aw, ah, bx, by, bw, bh float64) float64 {
	ix1 := math.Max(ax, bx)
	iy1 := math.Max(ay, by)
	ix2 := math.Min(ax+aw, bx+bw)
	iy2 := math.Min(ay+ah, by+bh)
	iw := math.Max(0, ix2-ix1)
	ih := math.Max(0, iy2-iy1)
	inter := iw * ih
	union := aw*ah + bw*bh - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// readPPSubjects loads non-deleted, named PhotoPrism subjects.
func (v *verifier) readPPSubjects(ctx context.Context) ([]ppSubjectRow, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx, `
		SELECT subj_uid, subj_name FROM subjects
		WHERE deleted_at IS NULL AND subj_name <> ''
		ORDER BY subj_uid`)
	if err != nil {
		return nil, fmt.Errorf("query pp subjects: %w", err)
	}
	defer rows.Close()
	var out []ppSubjectRow
	for rows.Next() {
		var s ppSubjectRow
		if err := rows.Scan(&s.UID, &s.Name); err != nil {
			return nil, fmt.Errorf("scan pp subject: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp subjects: %w", err)
	}
	return out, nil
}

// readPPMarkers loads every marker attached to a non-missing file. The
// projection matches what the verifier needs for geometry/count diffs;
// invalid/reviewed flags are intentionally not consulted because the
// migrator preserves them verbatim and the spec only asks for geometry.
func (v *verifier) readPPMarkers(ctx context.Context) ([]ppMarkerRow, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx, `
		SELECT marker_uid, COALESCE(file_uid, ''), COALESCE(subj_uid, ''),
		       COALESCE(x, 0), COALESCE(y, 0),
		       COALESCE(w, 0), COALESCE(h, 0)
		FROM markers
		WHERE file_uid IS NOT NULL AND file_uid <> ''`)
	if err != nil {
		return nil, fmt.Errorf("query pp markers: %w", err)
	}
	defer rows.Close()
	var out []ppMarkerRow
	for rows.Next() {
		var (
			mk                          ppMarkerRow
			markerUID, fileUID, subjUID []byte
		)
		if err := rows.Scan(&markerUID, &fileUID, &subjUID, &mk.X, &mk.Y, &mk.W, &mk.H); err != nil {
			return nil, fmt.Errorf("scan pp marker: %w", err)
		}
		mk.MarkerUID = string(markerUID)
		mk.FileUID = string(fileUID)
		mk.SubjectUID = string(subjUID)
		out = append(out, mk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp markers: %w", err)
	}
	return out, nil
}
