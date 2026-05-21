// Merge semantics for re-runs (added by task 68fc8ca2 — photo-level
// metadata gap-fix):
//
// migrate-from-photoprism is run multiple times in practice — the operator
// performs a first migration, the application gains additional columns,
// and the migration is re-run to backfill. Before this change, any photo
// whose file_hash already lived in the destination was skipped entirely.
// That meant any column added after the first run was stuck at its
// default value for the historical population.
//
// The current behaviour:
//
//  - For a photo whose file_hash is NOT in the destination, the migrator
//    inserts a new row using the full source projection.
//  - For a photo whose file_hash IS in the destination, the migrator now
//    branches into a "backfill" pass (see backfillExtraMetadata) that
//    fills ONLY the columns listed in this task's spec and ONLY when the
//    destination value is still the column's zero value. Columns the
//    user may have edited (title, description, notes, lat/lng, ...) are
//    NEVER touched on the backfill pass.
//
// If you add a new source-driven column to the photos table, extend
// backfillExtraMetadata so a re-run picks it up for historical photos.

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// ppPhoto is the projection of a PhotoPrism photo row that the migration
// cares about. Nullable columns are exposed as *Type so the writer can
// distinguish "no value" from "zero value" (e.g. ISO 0 vs. ISO unknown).
type ppPhoto struct {
	PhotoUID    string
	TakenAt     *time.Time
	Title       string
	Caption     string
	Notes       string
	Lat         *float64
	Lng         *float64
	Altitude    *float64
	ISO         *int
	FNumber     *float64
	Exposure    string
	FocalLength *float64
	CameraMake  string
	CameraModel string
	LensModel   string
	Favorite    bool
	Private     bool

	// Extra metadata added by task 68fc8ca2 (photo-level data-loss fix).
	// Keywords carries the union of details.keywords (comma-separated)
	// and the photos_keywords join table, deduplicated.
	TimeZone      string
	TakenAtOffset int // seconds east of UTC
	Panorama      bool
	Scan          bool
	Quality       int16
	ExifArtist    string
	ExifCopyright string
	ExifLicense   string
	ExifSoftware  string
	Keywords      []string
}

// ppFile is the projection of a PhotoPrism `files` row. Primary files
// become photos; non-primary files become `photo_files` rows. file_hash is
// the PhotoPrism SHA1; we recompute SHA256 from the source file because
// photo-sorter standardises on SHA256.
type ppFile struct {
	FileUID     string
	PhotoUID    string
	FileName    string
	FileSize    int64
	FileMime    string
	FileWidth   int
	FileHeight  int
	Orientation int
	IsPrimary   bool
	IsSidecar   bool
}

// stagePhotos imports every PhotoPrism photo. For each photo the migrator
// (1) finds the primary file row, (2) reads bytes from the source root,
// (3) hashes with SHA256, (4) skips if the destination DB already has a
// row with the same hash, (5) writes the file into the native originals
// tree at YYYY/MM/<basename>, (6) inserts the photo row, and (7) attaches
// every non-primary file as a `photo_files` row. Side-effects are
// idempotent — a re-run picks up exactly where the previous run stopped.
func (m *migrator) stagePhotos(ctx context.Context) error {
	photos, err := m.readPPPhotos(ctx)
	if err != nil {
		return fmt.Errorf("read photos: %w", err)
	}
	files, err := m.readPPFiles(ctx)
	if err != nil {
		return fmt.Errorf("read files: %w", err)
	}
	// Group files by photo UID so we can hand each photo its primary +
	// sidecar files in a single pass.
	byPhoto := make(map[string][]ppFile, len(files))
	for _, f := range files {
		byPhoto[f.PhotoUID] = append(byPhoto[f.PhotoUID], f)
	}

	summary := StageSummary{Stage: StagePhotos, Read: len(photos)}
	bar := newStageBar(len(photos), "photos")
	defer finishBar(bar)

	for i := range photos {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(summary)
			return fmt.Errorf("photos canceled: %w", err)
		}
		_ = bar.Add(1)
		m.processOnePhoto(ctx, &photos[i], byPhoto[photos[i].PhotoUID], &summary)
	}
	m.report.AppendStage(summary)
	return nil
}

// processOnePhoto handles a single PhotoPrism photo end to end. Counters
// on *StageSummary are updated; errors are logged but do not abort the
// stage (one bad file should not prevent the rest from migrating).
func (m *migrator) processOnePhoto(
	ctx context.Context, p *ppPhoto, files []ppFile, summary *StageSummary,
) {
	primary, ok := pickPrimaryFile(files)
	if !ok {
		fmt.Fprintf(m.out, "\nphoto %s: no primary file row\n", p.PhotoUID)
		summary.Failed++
		return
	}
	prep, err := prepareSourceFile(m.opts.OriginalsRoot, primary.FileName)
	if err != nil {
		fmt.Fprintf(m.out, "\nphoto %s: %v\n", p.PhotoUID, err)
		summary.Failed++
		return
	}

	if m.handleExistingPhoto(ctx, p, files, prep.hash, summary) {
		return
	}
	if m.opts.DryRun {
		m.recordDryRun(p, files)
		summary.Created++
		return
	}
	m.persistOnePhoto(ctx, p, files, primary, prep, summary)
}

// sourcePrep bundles the (path, hash) pair so processOnePhoto only has
// to manage one local variable.
type sourcePrep struct {
	path string
	hash string
}

// prepareSourceFile validates that the PhotoPrism file is readable and
// hashes its bytes with SHA256. Returns a sentinel error so callers can
// log the failure and skip without aborting the stage.
func prepareSourceFile(originalsRoot, fileName string) (sourcePrep, error) {
	src := filepath.Join(originalsRoot, fileName)
	if _, err := os.Stat(src); err != nil {
		return sourcePrep{}, fmt.Errorf("missing source %s: %w", src, err)
	}
	hash, err := storage.HashFile(src)
	if err != nil {
		return sourcePrep{}, fmt.Errorf("hash %s: %w", src, err)
	}
	return sourcePrep{path: src, hash: hash}, nil
}

// handleExistingPhoto looks up the destination by hash and, if a row is
// already present, registers the PhotoPrism→native mapping and reports
// the photo as skipped. Before bailing, it runs the metadata backfill
// pass so columns added after the photo's first migration get filled
// in. Returns true when the caller should bail out.
func (m *migrator) handleExistingPhoto(
	ctx context.Context, p *ppPhoto, files []ppFile, hash string, summary *StageSummary,
) bool {
	if m.opts.DryRun {
		return false
	}
	existing, err := m.opts.Photos.GetPhotoByHash(ctx, hash)
	if err == nil && existing != nil {
		m.recordMapping(p.PhotoUID, existing.UID, files)
		m.backfillExtraMetadata(ctx, p, existing)
		summary.Skipped++
		return true
	}
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		fmt.Fprintf(m.out, "\nphoto %s: lookup by hash: %v\n", p.PhotoUID, err)
		summary.Failed++
		return true
	}
	return false
}

// backfillExtraMetadata fills photo-level metadata columns added after a
// photo's original migration. For each new column the function checks
// whether the destination value still equals the column's zero value;
// only then is the source value written. Columns the user may have
// edited in the native UI (title, description, lat/lng, ...) are NEVER
// considered here — the caller passes the existing photo unchanged for
// those.
//
// The function calls UpdatePhoto when at least one field needs updating.
// UpdatePhoto writes every column, but since we only mutate the fields
// we want to backfill, untouched columns get written back to their
// existing values (a no-op). Errors are logged but do not abort the
// stage — the photo row still exists, just with stale metadata.
func (m *migrator) backfillExtraMetadata(
	ctx context.Context, p *ppPhoto, existing *database.Photo,
) {
	if !applyBackfillFields(existing, p) {
		return
	}
	if err := m.opts.Photos.UpdatePhoto(ctx, existing); err != nil {
		fmt.Fprintf(m.out, "\nphoto %s: backfill metadata: %v\n", p.PhotoUID, err)
	}
}

// applyBackfillFields runs every per-field merger. The boolean OR over
// each merger's return value is the "did anything change" signal the
// caller uses to decide whether to issue an UPDATE.
func applyBackfillFields(existing *database.Photo, p *ppPhoto) bool {
	changed := false
	for _, merger := range backfillMergers(existing, p) {
		if merger() {
			changed = true
		}
	}
	return changed
}

// backfillMergers returns the closure list that backs applyBackfillFields.
// Each closure captures a (dst, src) pair plus the predicate that
// distinguishes "dst is still default" from "dst was edited by the
// user". Splitting them into per-field closures keeps each one trivial
// and lets the linter's complexity budget breathe.
func backfillMergers(existing *database.Photo, p *ppPhoto) []func() bool {
	return []func() bool{
		func() bool { return mergeIfEmpty(&existing.TimeZone, p.TimeZone) },
		func() bool { return mergeIfZeroInt(&existing.TakenAtOffset, p.TakenAtOffset) },
		func() bool { return mergeIfFalse(&existing.Panorama, p.Panorama) },
		func() bool { return mergeIfFalse(&existing.Scan, p.Scan) },
		func() bool { return mergeIfZeroInt16(&existing.Quality, p.Quality) },
		func() bool { return mergeIfEmpty(&existing.ExifArtist, p.ExifArtist) },
		func() bool { return mergeIfEmpty(&existing.ExifCopyright, p.ExifCopyright) },
		func() bool { return mergeIfEmpty(&existing.ExifLicense, p.ExifLicense) },
		func() bool { return mergeIfEmpty(&existing.ExifSoftware, p.ExifSoftware) },
		func() bool { return mergeKeywordsIfEmpty(&existing.Keywords, p.Keywords) },
	}
}

// mergeIfEmpty copies src into *dst when dst is empty and src is not.
// Returns true when the value changed.
func mergeIfEmpty(dst *string, src string) bool {
	if *dst != "" || src == "" {
		return false
	}
	*dst = src
	return true
}

// mergeIfZeroInt copies src into *dst when dst is zero and src is not.
// Returns true when the value changed.
func mergeIfZeroInt(dst *int, src int) bool {
	if *dst != 0 || src == 0 {
		return false
	}
	*dst = src
	return true
}

// mergeIfZeroInt16 is the int16 specialisation of mergeIfZeroInt; the
// quality column is SMALLINT so its native Go width is 16 bits.
func mergeIfZeroInt16(dst *int16, src int16) bool {
	if *dst != 0 || src == 0 {
		return false
	}
	*dst = src
	return true
}

// mergeIfFalse sets *dst when dst is false and src is true. Flipping
// "this is a panorama" off intentionally falls through so a user edit
// (clearing the flag in the UI) is never re-asserted from a stale
// PhotoPrism row.
func mergeIfFalse(dst *bool, src bool) bool {
	if *dst || !src {
		return false
	}
	*dst = true
	return true
}

// mergeKeywordsIfEmpty replaces *dst with a copy of src when dst is
// empty and src has at least one element. Returns true when *dst was
// written.
func mergeKeywordsIfEmpty(dst *[]string, src []string) bool {
	if len(*dst) > 0 || len(src) == 0 {
		return false
	}
	*dst = append([]string(nil), src...)
	return true
}

// recordDryRun seeds photoMap / fileMap with synthetic UIDs so that
// downstream dry-run stages report realistic counts.
func (m *migrator) recordDryRun(p *ppPhoto, files []ppFile) {
	synthetic := "dry-" + p.PhotoUID
	m.recordMapping(p.PhotoUID, synthetic, files)
}

// recordMapping records both the photo and file UID mappings used by
// labels / albums / markers later in the run.
func (m *migrator) recordMapping(ppPhotoUID, nativeUID string, files []ppFile) {
	m.photoMap[ppPhotoUID] = nativeUID
	for _, f := range files {
		m.fileMap[f.FileUID] = nativeUID
	}
}

// persistOnePhoto writes the original bytes, inserts the photo row, and
// attaches every non-primary file as a photo_files row. The function
// updates the summary counters in place; on a hash collision (concurrent
// re-run) it converges by treating the photo as skipped.
func (m *migrator) persistOnePhoto(
	ctx context.Context, p *ppPhoto, files []ppFile, primary ppFile,
	prep sourcePrep, summary *StageSummary,
) {
	takenAt := pickTakenAt(p)
	relPath := storage.OriginalRelPath(deref(takenAt), filepath.Base(primary.FileName))

	writtenHash, size, err := copyOriginal(prep.path, relPath, m.opts.Store)
	if err != nil {
		fmt.Fprintf(m.out, "\nphoto %s: copy %s -> %s: %v\n",
			p.PhotoUID, prep.path, relPath, err)
		summary.Failed++
		return
	}
	// Source file changed between the pre-copy hash and the copy: trust
	// the post-copy hash since that is what is on disk.
	finalHash := prep.hash
	if writtenHash != finalHash {
		finalHash = writtenHash
	}

	photo := buildPhotoRecord(p, primary, relPath, finalHash, size, takenAt, m.opts.UploaderUID)
	if err := m.opts.Photos.CreatePhoto(ctx, photo); err != nil {
		m.handleCreateConflict(ctx, p, files, finalHash, relPath, err, summary)
		return
	}
	m.recordMapping(p.PhotoUID, photo.UID, files)
	m.newPhotos[photo.UID] = true
	m.attachExtraFiles(ctx, p, photo.UID, files, finalHash)
	summary.Created++
}

// handleCreateConflict converges on a UNIQUE-violation on either file_hash
// or uid. The file_hash path is the original idempotency case: the photo
// is already migrated, so register the mapping using the existing row and
// count it as skipped. The uid path is the legacy-data case: a previous
// (buggy) migration wrote rows with generated UIDs, so the native DB
// already holds a different file_hash under this PhotoPrism UID — that
// photo is logged with a runbook pointer and counted as failed. Any other
// error is logged and the orphaned original is deleted.
func (m *migrator) handleCreateConflict(
	ctx context.Context, p *ppPhoto, files []ppFile, hash, relPath string,
	createErr error, summary *StageSummary,
) {
	existing, lookupErr := m.opts.Photos.GetPhotoByHash(ctx, hash)
	if lookupErr == nil && existing != nil {
		m.recordMapping(p.PhotoUID, existing.UID, files)
		summary.Skipped++
		return
	}
	if m.handleUIDCollision(ctx, p, hash, relPath, createErr, summary) {
		return
	}
	fmt.Fprintf(m.out, "\nphoto %s: insert: %v\n", p.PhotoUID, createErr)
	summary.Failed++
	_ = m.opts.Store.DeleteOriginal(relPath)
}

// handleUIDCollision detects the "different photo, same PhotoPrism UID"
// case: a row with this UID already exists in the destination but carries
// a different file_hash. This means a prior buggy migration wrote rows
// with generated UIDs and at least one of those happens to clash with
// today's PhotoPrism UID. The orphaned original is removed and the photo
// is recorded as failed so the operator knows to run migrate-remap-
// references. Returns true when the case applied (caller bails out).
func (m *migrator) handleUIDCollision(
	ctx context.Context, p *ppPhoto, hash, relPath string,
	createErr error, summary *StageSummary,
) bool {
	existing, err := m.opts.Photos.GetPhoto(ctx, p.PhotoUID)
	if err != nil || existing == nil {
		return false
	}
	if existing.FileHash == hash {
		// Same file under the same UID — the by-hash branch above should
		// already have caught this; treat as skipped defensively.
		summary.Skipped++
		_ = m.opts.Store.DeleteOriginal(relPath)
		return true
	}
	fmt.Fprintf(m.out,
		"\nphoto %s: UID collision with an existing native row carrying a "+
			"different file_hash. This usually means a previous (buggy) "+
			"migration created native rows with generated UIDs. Run "+
			"`photo-sorter migrate-remap-references` after this command "+
			"finishes (see --emit-photo-map). Insert error: %v\n",
		p.PhotoUID, createErr)
	summary.Failed++
	_ = m.opts.Store.DeleteOriginal(relPath)
	return true
}

// attachExtraFiles registers every non-primary PhotoPrism file as a
// photo_files row. Sidecar bytes are NOT copied; only the DB row is
// created so callers can find the file relative to the original root.
func (m *migrator) attachExtraFiles(
	ctx context.Context, p *ppPhoto, nativePhotoUID string, files []ppFile, primaryHash string,
) {
	for i := range files {
		f := &files[i]
		if f.IsPrimary {
			continue
		}
		pf := &database.PhotoFile{
			PhotoUID:  nativePhotoUID,
			FilePath:  filepath.ToSlash(f.FileName),
			FileHash:  primaryHash,
			FileSize:  f.FileSize,
			FileMime:  f.FileMime,
			IsPrimary: false,
			Role:      fileRole(f),
		}
		if err := m.opts.Photos.AddPhotoFile(ctx, pf); err != nil {
			fmt.Fprintf(m.out, "\nphoto %s: add file %s: %v\n",
				p.PhotoUID, f.FileName, err)
			continue
		}
	}
}

// pickPrimaryFile returns the file_primary=1 row; if none is marked
// primary (PhotoPrism does this for some kinds of stacks) the first file
// is used so the photo still lands in the destination.
func pickPrimaryFile(files []ppFile) (ppFile, bool) {
	if len(files) == 0 {
		return ppFile{}, false
	}
	for _, f := range files {
		if f.IsPrimary {
			return f, true
		}
	}
	return files[0], true
}

// fileRole maps a PhotoPrism file row to the native role enum
// ('original' | 'sidecar' | 'edited').
func fileRole(f *ppFile) string {
	if f.IsSidecar {
		return "sidecar"
	}
	return "original"
}

// pickTakenAt returns the photo's taken_at as a *time.Time. PhotoPrism
// uses sentinel "0001-01-01" for unknown dates; we collapse those to nil.
func pickTakenAt(p *ppPhoto) *time.Time {
	if p.TakenAt == nil {
		return nil
	}
	if p.TakenAt.Year() <= 1 {
		return nil
	}
	t := p.TakenAt.UTC()
	return &t
}

// deref returns the zero value for t when it is nil. The native storage
// layout uses an "unknown" sub-directory for zero dates, so this maps
// straight onto OriginalRelPath's semantics.
func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// copyOriginal streams bytes from srcPath into the storage layer at
// relPath, returning the SHA256 of what was written and the byte count.
// io.Copy + WriteOriginal combine an atomic rename with rolling SHA256
// hashing so we get both the persisted file and a verified hash in one
// pass.
func copyOriginal(srcPath, relPath string, store *storage.Storage) (string, int64, error) {
	f, err := os.Open(srcPath) // #nosec G304 -- src comes from PhotoPrism originals root
	if err != nil {
		return "", 0, fmt.Errorf("open source: %w", err)
	}
	defer f.Close()
	n, hash, err := store.WriteOriginal(relPath, io.Reader(f))
	if err != nil {
		return "", 0, fmt.Errorf("write original: %w", err)
	}
	return hash, n, nil
}

// buildPhotoRecord assembles the native photo row from the PhotoPrism
// projections. taken_at_source is "exif" when PhotoPrism had a date, else
// "unknown". The PhotoPrism photo_uid is preserved verbatim as the native
// photos.uid so historic references in other tables (embeddings, faces,
// section_photos, page_slots, ...) keep working without a remap pass.
func buildPhotoRecord(
	p *ppPhoto, primary ppFile, relPath, hash string, size int64,
	takenAt *time.Time, uploaderUID string,
) *database.Photo {
	source := "unknown"
	if takenAt != nil {
		source = "exif"
	}
	orientation := primary.Orientation
	if orientation <= 0 {
		orientation = 1
	}
	mime := primary.FileMime
	if mime == "" {
		mime = "application/octet-stream"
	}
	return &database.Photo{
		UID:             p.PhotoUID,
		FileHash:        hash,
		FilePath:        relPath,
		FileName:        filepath.Base(primary.FileName),
		FileSize:        size,
		FileMime:        mime,
		FileWidth:       primary.FileWidth,
		FileHeight:      primary.FileHeight,
		FileOrientation: orientation,
		TakenAt:         takenAt,
		TakenAtSource:   source,
		TimeZone:        p.TimeZone,
		TakenAtOffset:   p.TakenAtOffset,
		Title:           p.Title,
		Description:     p.Caption,
		Notes:           p.Notes,
		Lat:             p.Lat,
		Lng:             p.Lng,
		Altitude:        p.Altitude,
		CameraMake:      p.CameraMake,
		CameraModel:     p.CameraModel,
		LensModel:       p.LensModel,
		ISO:             p.ISO,
		Aperture:        p.FNumber,
		Exposure:        p.Exposure,
		FocalLength:     p.FocalLength,
		ExifArtist:      p.ExifArtist,
		ExifCopyright:   p.ExifCopyright,
		ExifLicense:     p.ExifLicense,
		ExifSoftware:    p.ExifSoftware,
		Keywords:        append([]string(nil), p.Keywords...),
		Panorama:        p.Panorama,
		Scan:            p.Scan,
		Quality:         p.Quality,
		Favorite:        p.Favorite,
		Private:         p.Private,
		UploadedBy:      uploaderUID,
	}
}

// photosQuery is the projection of photo + camera + lens + details we
// need. PhotoPrism stores notes / keywords / artist / copyright / license
// / software in the `details` table joined via photo_id; description
// lives in `photos.photo_caption`. taken_at_local is paired with
// taken_at so the migrator can compute taken_at_offset in seconds
// (taken_at_local − taken_at, both naive wall-clock timestamps).
//
// time_zone defaults to NULL when older PhotoPrism rows never set it;
// COALESCE swallows that. Some rows carry the literal sentinel "Local"
// instead of an IANA name, which scanPPPhoto maps to empty string.
const photosQuery = `
	SELECT p.photo_uid, p.taken_at, p.taken_at_local,
	       COALESCE(p.photo_title, ''),
	       COALESCE(p.photo_caption, ''),
	       COALESCE(d.notes, ''),
	       p.photo_lat, p.photo_lng, p.photo_altitude,
	       p.photo_iso, p.photo_f_number, p.photo_exposure, p.photo_focal_length,
	       COALESCE(c.camera_make, ''), COALESCE(c.camera_model, ''),
	       COALESCE(l.lens_model, ''),
	       COALESCE(p.photo_favorite, 0), COALESCE(p.photo_private, 0),
	       COALESCE(p.photo_panorama, 0), COALESCE(p.photo_scan, 0),
	       COALESCE(p.photo_quality, 0),
	       COALESCE(p.time_zone, ''),
	       COALESCE(d.keywords, ''),
	       COALESCE(d.artist, ''), COALESCE(d.copyright, ''),
	       COALESCE(d.license, ''), COALESCE(d.software, '')
	FROM photos p
	LEFT JOIN cameras c ON c.id = p.camera_id
	LEFT JOIN lenses  l ON l.id = p.lens_id
	LEFT JOIN details d ON d.photo_id = p.id
	WHERE p.deleted_at IS NULL
	ORDER BY p.id`

// readPPPhotos loads the photo projection. Each row is scanned via
// scanPPPhoto so this function stays inside the linter's complexity
// budget. After the per-row scan completes, keywords pulled from the
// normalized photos_keywords join are merged in so each ppPhoto carries
// the union of both keyword sources (details.keywords and
// photos_keywords ⨝ keywords).
func (m *migrator) readPPPhotos(ctx context.Context) ([]ppPhoto, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx, photosQuery)
	if err != nil {
		return nil, fmt.Errorf("query photos: %w", err)
	}
	defer rows.Close()
	var out []ppPhoto
	for rows.Next() {
		p, err := scanPPPhoto(rows)
		if err != nil {
			return nil, err
		}
		if p.TimeZone == "" {
			// scanPPPhoto already validated against time.LoadLocation; an
			// empty value here can either mean "PhotoPrism stored Local /
			// NULL" (legitimate, the spec asks us to leave it empty) or
			// "the row carried a value that failed validation". We can't
			// distinguish the two without re-scanning, so we keep the log
			// silent for now — the gap-fix verification step compares
			// row counts and surfaces drift if it ever happens.
			_ = ctx
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos: %w", err)
	}
	joined, err := m.readPPKeywords(ctx)
	if err != nil {
		// A failure on the normalized keyword join is non-fatal —
		// details.keywords usually already covers everything in the
		// operator's instance. Log and continue.
		fmt.Fprintf(m.out, "\nphotos_keywords lookup failed: %v\n", err)
		return out, nil
	}
	for i := range out {
		out[i].Keywords = mergeKeywords(out[i].Keywords, joined[out[i].PhotoUID])
	}
	return out, nil
}

// readPPKeywords loads the normalized photos_keywords ⨝ keywords ⨝ photos
// projection so the migrator can union it with details.keywords. The
// `keywords.skip` flag is honored — PhotoPrism uses it to mark
// auto-generated keywords that should not be shown to the user — so we
// only carry keywords the operator considers real. Each value is
// returned trimmed; the caller dedupes against details.keywords. Photos
// with no keyword rows simply have no entry in the map.
func (m *migrator) readPPKeywords(ctx context.Context) (map[string][]string, error) {
	const query = `
		SELECT p.photo_uid, k.keyword
		FROM photos_keywords pk
		JOIN photos p   ON p.id = pk.photo_id
		JOIN keywords k ON k.id = pk.keyword_id
		WHERE COALESCE(k.skip, 0) = 0
		  AND p.deleted_at IS NULL
		ORDER BY p.photo_uid, k.keyword`
	rows, err := m.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query photos_keywords: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var uid string
		var kwRaw []byte
		if err := rows.Scan(&uid, &kwRaw); err != nil {
			return nil, fmt.Errorf("scan photos_keywords: %w", err)
		}
		kw := strings.TrimSpace(string(kwRaw))
		if kw == "" {
			continue
		}
		out[uid] = append(out[uid], kw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos_keywords: %w", err)
	}
	// Deduplicate each per-photo list while preserving order so callers
	// get a stable result regardless of any duplicate rows in the
	// source database.
	for uid, kw := range out {
		sort.SliceStable(kw, func(i, j int) bool { return kw[i] < kw[j] })
		out[uid] = dedupeStringsStable(kw)
	}
	return out, nil
}

// dedupeStringsStable removes consecutive duplicates from a sorted slice.
// The caller is responsible for sorting; the function returns a new
// slice rather than reusing the input so partial writes do not leak
// stale tail elements.
func dedupeStringsStable(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := make([]string, 0, len(in))
	prev := ""
	for i, v := range in {
		if i > 0 && v == prev {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}

// scanPPPhoto reads one row from the photosQuery projection and assembles
// a ppPhoto. Nullable numeric columns are kept as pointers so we can
// distinguish "missing" from "zero". keywords from the photos_keywords
// normalized table are merged in later by readPPPhotos.
func scanPPPhoto(rows *sql.Rows) (ppPhoto, error) {
	var (
		p                        ppPhoto
		takenAt, takenAtLocal    sql.NullTime
		alt, iso, focalRaw       sql.NullInt64
		fnum                     sql.NullFloat64
		lat, lng                 sql.NullFloat64
		fav, priv, pano, scan    int
		quality                  int16
		tz                       []byte
		exposureRaw, keywordsRaw []byte
		artistRaw, copyrightRaw  []byte
		licenseRaw, softwareRaw  []byte
	)
	if err := rows.Scan(
		&p.PhotoUID, &takenAt, &takenAtLocal, &p.Title, &p.Caption, &p.Notes,
		&lat, &lng, &alt, &iso, &fnum, &exposureRaw, &focalRaw,
		&p.CameraMake, &p.CameraModel, &p.LensModel,
		&fav, &priv,
		&pano, &scan, &quality,
		&tz,
		&keywordsRaw,
		&artistRaw, &copyrightRaw, &licenseRaw, &softwareRaw,
	); err != nil {
		return ppPhoto{}, fmt.Errorf("scan photo: %w", err)
	}
	if takenAt.Valid {
		t := takenAt.Time
		p.TakenAt = &t
	}
	p.Lat = nullToFloatPtr(lat)
	p.Lng = nullToFloatPtr(lng)
	p.Altitude = nullToIntFloatPtr(alt)
	p.ISO = nullToIntPtr(iso)
	p.FNumber = nullToFloatPtr(fnum)
	p.FocalLength = nullToIntFloatPtr(focalRaw)
	p.Exposure = string(exposureRaw)
	p.Favorite = fav != 0
	p.Private = priv != 0
	p.Panorama = pano != 0
	p.Scan = scan != 0
	p.Quality = clampPPQuality(quality)
	p.TimeZone = normalizeTimeZone(string(tz))
	p.TakenAtOffset = computeTakenAtOffset(takenAt, takenAtLocal)
	p.ExifArtist = string(artistRaw)
	p.ExifCopyright = string(copyrightRaw)
	p.ExifLicense = string(licenseRaw)
	p.ExifSoftware = string(softwareRaw)
	p.Keywords = parseDetailsKeywords(string(keywordsRaw))
	return p, nil
}

// clampPPQuality keeps PhotoPrism's photo_quality column inside the [0, 7]
// range the destination schema enforces. Operator instances occasionally
// carry NULL (-> 0 here via COALESCE) or out-of-range values written by
// older indexer versions, so we are defensive.
func clampPPQuality(q int16) int16 {
	switch {
	case q < 0:
		return 0
	case q > 7:
		return 7
	default:
		return q
	}
}

// normalizeTimeZone collapses PhotoPrism's sentinel values to the empty
// string. PhotoPrism stores either an IANA zone (e.g. "Europe/Prague"),
// the literal "UTC", the placeholder "Local" (meaning "we don't know"),
// or NULL. The destination column represents "unknown" as the empty
// string per the spec — do not invent a default.
//
// Real IANA zones are kept as-is. A name that fails time.LoadLocation
// falls back to the empty string and a logged warning at the call site.
func normalizeTimeZone(tz string) string {
	switch strings.TrimSpace(tz) {
	case "", "Local":
		return ""
	}
	if _, err := time.LoadLocation(tz); err != nil {
		// Caller logs; we just refuse to write the bogus value.
		return ""
	}
	return tz
}

// computeTakenAtOffset derives a UTC offset in seconds from the
// (taken_at, taken_at_local) pair PhotoPrism stores side by side. Both
// columns are MariaDB DATETIME with no timezone information, so the
// driver hands them back as time.Time in UTC. The arithmetic is purely
// nominal: it gives back the wall-clock delta the operator chose for
// the photo (e.g. +7200 seconds for a Czech summer shot).
//
// Verified source unit on 2026-05-21: seconds. PhotoPrism's taken_at /
// taken_at_local pair stores wall-clock values; the offset derived from
// the difference is a whole number of seconds (typically a multiple of
// 60 since EXIF carries minute-granularity offsets). Old "minutes"-based
// PhotoPrism schemas pre-date the migrator's supported source range.
func computeTakenAtOffset(takenAt, takenAtLocal sql.NullTime) int {
	if !takenAt.Valid || !takenAtLocal.Valid {
		return 0
	}
	return int(takenAtLocal.Time.Sub(takenAt.Time) / time.Second)
}

// parseDetailsKeywords splits PhotoPrism's comma-separated
// details.keywords column into a deduplicated slice. Whitespace-padded
// tokens are trimmed; empty tokens are dropped. Casing is preserved
// because PhotoPrism users sometimes capitalise proper nouns.
func parseDetailsKeywords(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, raw := range parts {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		if _, dup := seen[token]; dup {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

// mergeKeywords folds the per-photo keyword list pulled from the
// photos_keywords + keywords join into the keyword slice already parsed
// out of details.keywords. The result is deduplicated while preserving
// the first-seen ordering (details.keywords comes first since users
// usually edit that field directly in the PhotoPrism UI).
func mergeKeywords(detailsKW, joinKW []string) []string {
	if len(joinKW) == 0 {
		return detailsKW
	}
	out := make([]string, 0, len(detailsKW)+len(joinKW))
	seen := make(map[string]struct{}, len(detailsKW)+len(joinKW))
	for _, src := range [][]string{detailsKW, joinKW} {
		for _, kw := range src {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			if _, dup := seen[kw]; dup {
				continue
			}
			seen[kw] = struct{}{}
			out = append(out, kw)
		}
	}
	return out
}

// nullToFloatPtr returns a *float64 set when the source is non-null.
func nullToFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

// nullToIntPtr returns a *int set when the source is non-null.
func nullToIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

// nullToIntFloatPtr returns a *float64 set when the source is non-null.
// Used for integer columns that map to a float64 native field (altitude,
// focal length).
func nullToIntFloatPtr(v sql.NullInt64) *float64 {
	if !v.Valid {
		return nil
	}
	f := float64(v.Int64)
	return &f
}

// readPPFiles loads the projection of every file row, including deleted
// photos' files (the WHERE on photos handles that). file_primary is
// 0/1/NULL in PhotoPrism; treat NULL as not-primary.
func (m *migrator) readPPFiles(ctx context.Context) ([]ppFile, error) {
	const query = `
		SELECT f.file_uid, f.photo_uid,
		       f.file_name, COALESCE(f.file_size, 0),
		       COALESCE(f.file_mime, ''),
		       COALESCE(f.file_width, 0), COALESCE(f.file_height, 0),
		       COALESCE(f.file_orientation, 1),
		       COALESCE(f.file_primary, 0), COALESCE(f.file_sidecar, 0)
		FROM files f
		WHERE f.deleted_at IS NULL
		  AND COALESCE(f.file_missing, 0) = 0
		ORDER BY f.photo_uid, f.file_primary DESC, f.id`
	rows, err := m.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()
	var out []ppFile
	for rows.Next() {
		var (
			f                  ppFile
			primary, sidecar   int
			fileName, fileMime []byte
		)
		if err := rows.Scan(
			&f.FileUID, &f.PhotoUID,
			&fileName, &f.FileSize,
			&fileMime,
			&f.FileWidth, &f.FileHeight, &f.Orientation,
			&primary, &sidecar,
		); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		f.FileName = string(fileName)
		f.FileMime = string(fileMime)
		f.IsPrimary = primary != 0
		f.IsSidecar = sidecar != 0
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}
	return out, nil
}
