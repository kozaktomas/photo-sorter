package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// the photo as skipped. Returns true when the caller should bail out.
func (m *migrator) handleExistingPhoto(
	ctx context.Context, p *ppPhoto, files []ppFile, hash string, summary *StageSummary,
) bool {
	if m.opts.DryRun {
		return false
	}
	existing, err := m.opts.Photos.GetPhotoByHash(ctx, hash)
	if err == nil && existing != nil {
		m.recordMapping(p.PhotoUID, existing.UID, files)
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

// handleCreateConflict converges on a file_hash UNIQUE-violation by
// re-reading the conflicting row and treating the photo as skipped. Any
// other error is logged and the orphaned original is deleted.
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
	fmt.Fprintf(m.out, "\nphoto %s: insert: %v\n", p.PhotoUID, createErr)
	summary.Failed++
	_ = m.opts.Store.DeleteOriginal(relPath)
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
// "unknown".
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
		Favorite:        p.Favorite,
		Private:         p.Private,
		UploadedBy:      uploaderUID,
	}
}

// photosQuery is the projection of photo + camera + lens + details we
// need. PhotoPrism stores notes in the `details` table joined via
// photo_id; description lives in `photos.photo_caption`.
const photosQuery = `
	SELECT p.photo_uid, p.taken_at,
	       COALESCE(p.photo_title, ''),
	       COALESCE(p.photo_caption, ''),
	       COALESCE(d.notes, ''),
	       p.photo_lat, p.photo_lng, p.photo_altitude,
	       p.photo_iso, p.photo_f_number, p.photo_exposure, p.photo_focal_length,
	       COALESCE(c.camera_make, ''), COALESCE(c.camera_model, ''),
	       COALESCE(l.lens_model, ''),
	       COALESCE(p.photo_favorite, 0), COALESCE(p.photo_private, 0)
	FROM photos p
	LEFT JOIN cameras c ON c.id = p.camera_id
	LEFT JOIN lenses  l ON l.id = p.lens_id
	LEFT JOIN details d ON d.photo_id = p.id
	WHERE p.deleted_at IS NULL
	ORDER BY p.id`

// readPPPhotos loads the photo projection. Each row is scanned via
// scanPPPhoto so this function stays inside the linter's complexity
// budget.
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
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos: %w", err)
	}
	return out, nil
}

// scanPPPhoto reads one row from the photosQuery projection and assembles
// a ppPhoto. Nullable numeric columns are kept as pointers so we can
// distinguish "missing" from "zero".
func scanPPPhoto(rows *sql.Rows) (ppPhoto, error) {
	var (
		p           ppPhoto
		takenAt     sql.NullTime
		alt         sql.NullInt64
		iso         sql.NullInt64
		fnum        sql.NullFloat64
		focalRaw    sql.NullInt64
		lat, lng    sql.NullFloat64
		fav, priv   int
		exposureRaw []byte
	)
	if err := rows.Scan(
		&p.PhotoUID, &takenAt, &p.Title, &p.Caption, &p.Notes,
		&lat, &lng, &alt, &iso, &fnum, &exposureRaw, &focalRaw,
		&p.CameraMake, &p.CameraModel, &p.LensModel,
		&fav, &priv,
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
	return p, nil
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
