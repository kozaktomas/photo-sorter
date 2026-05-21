// Package verify compares an existing PhotoPrism instance against the
// photo-sorter native database to confirm that a one-shot migration moved
// everything correctly. It is read-only against both data sources and
// returns a structured report that the CLI layer formats as either a
// printable section-by-section table or JSON.
//
// The verifier intentionally re-derives identity for each entity in the
// same way the migrator did: photos are matched by SHA256 of the primary
// file, albums by slug, labels by slug, subjects by accent-insensitive
// lowercased name, and markers by approximate (file_uid → photo_uid)
// geometry overlap. Any discrepancy surfaces as a per-section list of
// "missing" / "orphan" / "geometry" entries, capped at 50 items each so
// the report stays printable even when an entire library is broken.
package verify

import (
	"context"
	"database/sql"
	"errors"
	"io"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// MaxItemsPerCategory caps the number of items reported under any single
// diff category (missing_in_sorter, orphan_in_sorter, geometry, ...).
// Tools that need every entry should use --json and post-process; the
// human-readable report stops at this many to keep the terminal output
// usable when a whole library is broken.
const MaxItemsPerCategory = 50

// DefaultConcurrency is the goroutine-pool size used for the SHA256
// rehash pass over PhotoPrism originals. Matches the spec.
const DefaultConcurrency = 4

// GeometryTolerance is the absolute coordinate tolerance for marker
// geometry diffs. Spec: report markers whose x/y/w/h differ by more than
// 1% on any axis.
const GeometryTolerance = 0.01

// Options bundles the runtime configuration of a verification run. The
// zero value is not usable — callers must set MariaDB, OriginalsRoot, and
// every read-only repository.
type Options struct {
	// MariaDB is the open *sql.DB pointing at the source PhotoPrism
	// instance. The verifier never closes it; the caller owns the
	// lifecycle.
	MariaDB *sql.DB

	// OriginalsRoot is the absolute path on disk where PhotoPrism stores
	// originals. PhotoPrism file_name columns include the YYYY/MM
	// prefix, so a single root is enough.
	OriginalsRoot string

	// Store is the photo-sorter destination storage layer. It provides
	// the originals root walk + AbsOriginal resolution that the disk
	// section uses to detect orphan files.
	Store *storage.Storage

	// Photos / Albums / Labels / Subjects / Markers are the native
	// repositories the verifier reads from. All are required.
	Photos   database.PhotoReader
	Albums   database.AlbumReader
	Labels   database.LabelReader
	Subjects database.SubjectReader
	Markers  database.MarkerReader

	// Concurrency overrides DefaultConcurrency for the photo hash worker
	// pool. Values <= 0 fall back to the default.
	Concurrency int

	// Writer receives progress lines as sections start/finish. nil
	// disables the chatter — the JSON path uses it that way.
	Writer io.Writer

	// FieldsOnly skips the structural diff (existence / counts / disk)
	// and runs only the field-level diff phase. Useful when iterating
	// on migrator fixes: the structural diff is expensive (rehashes
	// every original) while the field diff is just a JOIN.
	FieldsOnly bool

	// Strict, when true, drops every tolerance band: 1-second drift on
	// taken_at, 1e-6 drift on lat/lng, 1 m drift on altitude, and 0.01
	// drift on marker score all become diffs. Default off because real
	// migrations always pick up a few sub-second / sub-mm shifts.
	Strict bool
}

// Validate checks the must-have fields. It does NOT touch the network or
// the disk; callers that need a richer check should follow up with a
// connectivity probe.
func (o *Options) Validate() error {
	if o.MariaDB == nil {
		return errors.New("verify: Options.MariaDB is required")
	}
	if o.OriginalsRoot == "" {
		return errors.New("verify: Options.OriginalsRoot is required")
	}
	if o.Store == nil {
		return errors.New("verify: Options.Store is required")
	}
	if o.Photos == nil || o.Albums == nil || o.Labels == nil ||
		o.Subjects == nil || o.Markers == nil {
		return errors.New("verify: all repositories are required")
	}
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}
	return nil
}

// Run executes every verification section and returns the aggregated
// report. A canceled context aborts cooperatively — any partially
// populated section is preserved.
func Run(ctx context.Context, opts *Options) (*Report, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	v := &verifier{opts: opts, report: &Report{}}
	if err := v.run(ctx); err != nil {
		return v.report, err
	}
	return v.report, nil
}

// Report is the aggregated outcome of a verification run. Each top-level
// section corresponds to one of the requirement bullets in the spec.
type Report struct {
	Photos   PhotoReport   `json:"photos"`
	Albums   AlbumReport   `json:"albums"`
	Labels   LabelReport   `json:"labels"`
	Subjects SubjectReport `json:"subjects"`
	Markers  MarkerReport  `json:"markers"`
	Disk     DiskReport    `json:"disk"`
}

// HasDiffs reports whether any section recorded a discrepancy. The CLI
// uses this to pick the exit code so scripts can chain `migrate-verify`
// into the rest of their automation.
func (r *Report) HasDiffs() bool {
	return r.Photos.hasDiffs() ||
		r.Albums.hasDiffs() ||
		r.Labels.hasDiffs() ||
		r.Subjects.hasDiffs() ||
		r.Markers.hasDiffs() ||
		r.Disk.hasDiffs()
}

// PhotoReport is the photos section of the verification report.
type PhotoReport struct {
	PPCount         int             `json:"pp_count"`
	SorterCount     int             `json:"sorter_count"`
	MissingInSorter []string        `json:"missing_in_sorter"`
	OrphanInSorter  []string        `json:"orphan_in_sorter"`
	FieldDiffs      FieldDiffBucket `json:"field_diffs"`
}

// hasDiffs reports whether the photos section recorded any difference.
func (r *PhotoReport) hasDiffs() bool {
	return len(r.MissingInSorter) > 0 || len(r.OrphanInSorter) > 0 || r.FieldDiffs.HasDiffs()
}

// AlbumReport is the albums section of the report. PhotoDiffs is the
// per-album symmetric difference of photo memberships. MembershipDiffs
// is the widened per-pair diff (photo file_hash + album slug) so a
// missing membership is reported by identity, not just count.
type AlbumReport struct {
	PPCount           int                  `json:"pp_count"`
	SorterCount       int                  `json:"sorter_count"`
	SlugTitleMismatch []AlbumDiff          `json:"slug_title_mismatch"`
	MissingInSorter   []string             `json:"missing_in_sorter"`
	OrphanInSorter    []string             `json:"orphan_in_sorter"`
	PhotoDiffs        []AlbumPhoto         `json:"photo_diffs"`
	FieldDiffs        FieldDiffBucket      `json:"field_diffs"`
	MembershipDiffs   []MembershipPairDiff `json:"membership_diffs"`
}

// hasDiffs reports whether the albums section recorded any difference.
func (r *AlbumReport) hasDiffs() bool {
	return len(r.SlugTitleMismatch) > 0 ||
		len(r.MissingInSorter) > 0 ||
		len(r.OrphanInSorter) > 0 ||
		len(r.PhotoDiffs) > 0 ||
		r.FieldDiffs.HasDiffs() ||
		len(r.MembershipDiffs) > 0
}

// MembershipPairDiff is one (photo, container) pair that exists on one
// side but not the other. Used by both album and label junction-table
// diffs. Side is either "pp_only" (in PhotoPrism but not the sorter) or
// "sorter_only" (in the sorter but not PhotoPrism).
type MembershipPairDiff struct {
	ContainerSlug string `json:"container_slug"`
	PhotoFileHash string `json:"photo_file_hash"`
	Side          string `json:"side"`
}

// AlbumDiff records a slug/title mismatch between the two stores.
type AlbumDiff struct {
	Slug       string `json:"slug"`
	PPTitle    string `json:"pp_title"`
	NativeUID  string `json:"native_uid"`
	NativeName string `json:"native_title"`
}

// AlbumPhoto records a per-album symmetric difference. PPCount and
// SorterCount are the membership totals on each side; MissingInSorter and
// OrphanInSorter are the truncated photo lists (capped to MaxItemsPerCategory).
type AlbumPhoto struct {
	Slug            string   `json:"slug"`
	PPCount         int      `json:"pp_count"`
	SorterCount     int      `json:"sorter_count"`
	MissingInSorter []string `json:"missing_in_sorter"`
	OrphanInSorter  []string `json:"orphan_in_sorter"`
}

// LabelReport is the labels section of the report. PhotoPairDiffs is the
// per-label photo-count comparison. MembershipDiffs is the per-pair
// (label slug, photo file_hash) diff that widens "1 fewer pair" into
// "which pair is missing".
type LabelReport struct {
	PPCount          int                  `json:"pp_count"`
	SorterCount      int                  `json:"sorter_count"`
	PPPhotoPairs     int                  `json:"pp_photo_pairs"`
	SorterPhotoPairs int                  `json:"sorter_photo_pairs"`
	SlugNameMismatch []LabelDiff          `json:"slug_name_mismatch"`
	MissingInSorter  []string             `json:"missing_in_sorter"`
	OrphanInSorter   []string             `json:"orphan_in_sorter"`
	PhotoPairDiffs   []LabelPairDiff      `json:"photo_pair_diffs"`
	FieldDiffs       FieldDiffBucket      `json:"field_diffs"`
	MembershipDiffs  []MembershipPairDiff `json:"membership_diffs"`
}

// hasDiffs reports whether the labels section recorded any difference.
func (r *LabelReport) hasDiffs() bool {
	return len(r.SlugNameMismatch) > 0 ||
		len(r.MissingInSorter) > 0 ||
		len(r.OrphanInSorter) > 0 ||
		len(r.PhotoPairDiffs) > 0 ||
		r.FieldDiffs.HasDiffs() ||
		len(r.MembershipDiffs) > 0
}

// LabelDiff records a slug/name mismatch between the two stores.
type LabelDiff struct {
	Slug       string `json:"slug"`
	PPName     string `json:"pp_name"`
	NativeUID  string `json:"native_uid"`
	NativeName string `json:"native_name"`
}

// LabelPairDiff records a per-label photo-count mismatch.
type LabelPairDiff struct {
	Slug        string `json:"slug"`
	PPCount     int    `json:"pp_count"`
	SorterCount int    `json:"sorter_count"`
}

// SubjectReport is the subjects/markers report. Marker geometry diffs
// live in their own MarkerReport so the JSON shape matches the spec.
type SubjectReport struct {
	PPCount         int             `json:"pp_count"`
	SorterCount     int             `json:"sorter_count"`
	MissingInSorter []string        `json:"missing_in_sorter"`
	OrphanInSorter  []string        `json:"orphan_in_sorter"`
	FieldDiffs      FieldDiffBucket `json:"field_diffs"`
}

// hasDiffs reports whether the subjects section recorded any difference.
func (r *SubjectReport) hasDiffs() bool {
	return len(r.MissingInSorter) > 0 || len(r.OrphanInSorter) > 0 || r.FieldDiffs.HasDiffs()
}

// MarkerReport collects per-subject marker count diffs and the geometry
// drift list.
type MarkerReport struct {
	PPCount       int                  `json:"pp_count"`
	SorterCount   int                  `json:"sorter_count"`
	CountDiffs    []MarkerCountDiff    `json:"count_diffs"`
	GeometryDiffs []MarkerGeometryDiff `json:"geometry_diffs"`
	FieldDiffs    FieldDiffBucket      `json:"field_diffs"`
}

// hasDiffs reports whether the markers section recorded any difference.
func (r *MarkerReport) hasDiffs() bool {
	return len(r.CountDiffs) > 0 || len(r.GeometryDiffs) > 0 || r.FieldDiffs.HasDiffs()
}

// MarkerCountDiff records a per-subject marker count mismatch.
type MarkerCountDiff struct {
	SubjectName string `json:"subject_name"`
	PPCount     int    `json:"pp_count"`
	SorterCount int    `json:"sorter_count"`
}

// MarkerGeometryDiff records a marker whose x/y/w/h differs by more than
// GeometryTolerance from its PhotoPrism counterpart.
type MarkerGeometryDiff struct {
	PhotoSorterUID string  `json:"sorter_photo_uid"`
	PPMarkerUID    string  `json:"pp_marker_uid"`
	PPX            float64 `json:"pp_x"`
	PPY            float64 `json:"pp_y"`
	PPW            float64 `json:"pp_w"`
	PPH            float64 `json:"pp_h"`
	SorterX        float64 `json:"sorter_x"`
	SorterY        float64 `json:"sorter_y"`
	SorterW        float64 `json:"sorter_w"`
	SorterH        float64 `json:"sorter_h"`
}

// DiskReport lists originals on disk that have no matching photo row.
type DiskReport struct {
	OrphanFiles []string `json:"orphan_files"`
}

// hasDiffs reports whether the disk section recorded any difference.
func (r *DiskReport) hasDiffs() bool {
	return len(r.OrphanFiles) > 0
}

// truncate caps a slice at MaxItemsPerCategory. Used by the section
// runners so the report stays printable even for a fully diverged pair.
func truncate(items []string) []string {
	if len(items) <= MaxItemsPerCategory {
		return items
	}
	return items[:MaxItemsPerCategory]
}
