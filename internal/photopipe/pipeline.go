// Package photopipe orchestrates ingestion of a single uploaded photo end to
// end: buffer the upload to a temp file, hash and detect the format, run the
// duplicate check, decode HEIC/RAW into a thumbnail-friendly intermediate,
// extract EXIF, write the original into the storage tree, persist the
// database rows, and generate the cached thumbnails.
//
// The package is pure Go (no HTTP) so it can be reused by the HTTP upload
// handler, the migration tooling, and any future batch ingestion code.
package photopipe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/exif"
	"github.com/kozaktomas/photo-sorter/internal/imgconvert"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
)

// ErrDuplicate is returned by Ingest when SkipDuplicates is set and a photo
// with the same SHA256 hash already exists. The error wraps the existing
// *database.Photo via the Existing field of *DuplicateError, so callers can
// errors.As the result and decide what to do (e.g. return the existing UID).
var ErrDuplicate = errors.New("photo already exists")

// ErrUnsupportedFormat is returned by Ingest when the uploaded file's format
// could not be identified (DetectFormat returned "unknown"). This is the
// terminal failure for any non-image upload.
var ErrUnsupportedFormat = errors.New("unsupported file format")

// DuplicateError carries the existing photo alongside ErrDuplicate so the
// caller can recover the record without a second database round-trip. It
// satisfies errors.Is(err, ErrDuplicate) and errors.As(err, &dup).
type DuplicateError struct {
	Existing *database.Photo
}

// Error implements the error interface.
func (e *DuplicateError) Error() string {
	if e.Existing == nil {
		return ErrDuplicate.Error()
	}
	return fmt.Sprintf("%s: uid=%s hash=%s", ErrDuplicate.Error(), e.Existing.UID, e.Existing.FileHash)
}

// Unwrap returns ErrDuplicate so errors.Is works as expected.
func (e *DuplicateError) Unwrap() error { return ErrDuplicate }

// Options controls a single Ingest call. Filename is the user-supplied
// original filename (used for the on-disk path and the filename-date EXIF
// fallback) and is required. UploadedBy is the user UID stored on the photo
// row; the empty string is allowed and persisted as NULL. Both GenerateThumbs
// and SkipDuplicates default to true in the spec; the bool fields here mean
// callers explicitly opt in. The HTTP upload handler will always set both
// to true; migration code can flip GenerateThumbs off.
type Options struct {
	// Filename is the original filename from the upload. It is sanitized
	// before being written into the originals tree.
	Filename string
	// UploadedBy is the user UID stored on photos.uploaded_by.
	UploadedBy string
	// GenerateThumbs controls whether thumb.GenerateAll runs after the
	// photo row is inserted. Default true in the spec — set false during
	// migration when the existing thumbnail cache will be reused as-is.
	GenerateThumbs bool
	// SkipDuplicates controls whether GetPhotoByHash is consulted before
	// writing. Default true in the spec — set false only for tests that
	// intentionally want a unique_violation from the DB layer.
	SkipDuplicates bool
}

// Pipeline owns the storage layer, the photo repository (write side), and a
// read-side handle for the duplicate check. The reader is broken out from
// the writer so future code paths can hand in a read replica or a cache.
type Pipeline struct {
	store  *storage.Storage
	repo   database.PhotoWriter
	reader database.PhotoReader
}

// New constructs a Pipeline. All three dependencies are required; the
// function panics on nil arguments because misconfiguration here is a
// programmer error, not a runtime condition the pipeline can recover from.
func New(store *storage.Storage, repo database.PhotoWriter, reader database.PhotoReader) *Pipeline {
	if store == nil {
		panic("photopipe: store must not be nil")
	}
	if repo == nil {
		panic("photopipe: repo must not be nil")
	}
	if reader == nil {
		panic("photopipe: reader must not be nil")
	}
	return &Pipeline{store: store, repo: repo, reader: reader}
}

// ingestPrep bundles the intermediate state produced by the read-only
// portion of the pipeline (buffering, format detection, duplicate check,
// decoding, EXIF extraction). Passing it as a struct keeps Ingest's body
// short enough for gocognit and makes the rollback path easy to reason
// about — every field is set before any side effect on disk or in the DB.
type ingestPrep struct {
	tmpPath   string
	decodable string
	filename  string
	fileHash  string
	fileSize  int64
	format    string
	meta      *exif.Metadata
	existing  *database.Photo
}

// Ingest runs the full upload pipeline on src and returns the resulting
// *database.Photo. The contract is documented on the package; see the
// numbered steps in the spec for the order of operations.
func (p *Pipeline) Ingest(ctx context.Context, src io.Reader, opts Options) (*database.Photo, error) {
	if src == nil {
		return nil, errors.New("photopipe: src must not be nil")
	}
	if opts.Filename == "" {
		return nil, errors.New("photopipe: opts.Filename must not be empty")
	}

	prep, cleanup, err := p.prepareUpload(ctx, src, opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if prep.existing != nil {
		return prep.existing, &DuplicateError{Existing: prep.existing}
	}

	relPath := p.resolveOriginalPath(prep)
	if err := p.writeOriginalFromTemp(prep.tmpPath, relPath); err != nil {
		return nil, fmt.Errorf("photopipe: write original %q: %w", relPath, err)
	}

	// From here on the original is on disk. If anything below fails the
	// deferred rollback removes it so we don't leave orphaned files behind.
	success := false
	defer func() {
		if !success {
			if delErr := p.store.DeleteOriginal(relPath); delErr != nil {
				log.Printf("photopipe: rollback delete original %q: %v", relPath, delErr)
			}
		}
	}()

	photoRecord, err := p.persistPhoto(ctx, relPath, prep, opts)
	if err != nil {
		return nil, err
	}

	p.generateThumbsBestEffort(prep, photoRecord, opts)

	success = true
	return photoRecord, nil
}

// prepareUpload runs steps 1-6 of the spec: buffer to temp, hash, detect
// format, look up duplicates, decode HEIC/RAW into a JPEG-compatible path,
// and extract EXIF. The returned cleanup removes the temp file and the
// decodable temp file (if any) and is always non-nil.
func (p *Pipeline) prepareUpload(ctx context.Context, src io.Reader, opts Options) (*ingestPrep, func(), error) {
	tmpPath, fileHash, fileSize, err := bufferAndHash(src, opts.Filename)
	if err != nil {
		return nil, func() {}, err
	}
	noopCleanup := func() {}
	rmTemp := func() { _ = os.Remove(tmpPath) }

	format := imgconvert.DetectFormat(tmpPath)
	if format == imgconvert.FormatUnknown {
		rmTemp()
		return nil, noopCleanup, fmt.Errorf("%w: %s", ErrUnsupportedFormat, opts.Filename)
	}

	existing, found, err := p.lookupDuplicate(ctx, fileHash, opts)
	if err != nil {
		rmTemp()
		return nil, noopCleanup, err
	}
	if found {
		return &ingestPrep{tmpPath: tmpPath, filename: opts.Filename, existing: existing}, rmTemp, nil
	}

	decodable, cleanupDecodable, err := imgconvert.EnsureDecodable(ctx, tmpPath)
	if err != nil {
		rmTemp()
		return nil, noopCleanup, fmt.Errorf("photopipe: decode: %w", err)
	}
	combined := func() {
		cleanupDecodable()
		rmTemp()
	}

	meta, err := extractMetadata(ctx, tmpPath, opts.Filename)
	if err != nil {
		combined()
		return nil, noopCleanup, fmt.Errorf("photopipe: exif: %w", err)
	}

	return &ingestPrep{
		tmpPath:   tmpPath,
		decodable: decodable,
		filename:  opts.Filename,
		fileHash:  fileHash,
		fileSize:  fileSize,
		format:    format,
		meta:      meta,
	}, combined, nil
}

// lookupDuplicate consults the reader by hash when opts.SkipDuplicates is
// set. Returns (photo, true, nil) when a duplicate exists; (nil, false, nil)
// when the lookup was skipped or returned ErrNotFound; (nil, false, err)
// for any other DB error.
func (p *Pipeline) lookupDuplicate(ctx context.Context, fileHash string, opts Options) (*database.Photo, bool, error) {
	if !opts.SkipDuplicates {
		return nil, false, nil
	}
	existing, err := p.reader.GetPhotoByHash(ctx, fileHash)
	if err == nil {
		return existing, true, nil
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("photopipe: duplicate check: %w", err)
}

// resolveOriginalPath computes the originals-tree relative path for the
// upload and disambiguates filename + date collisions with a short-hash
// suffix before the extension.
func (p *Pipeline) resolveOriginalPath(prep *ingestPrep) string {
	takenAt := time.Time{}
	if prep.meta != nil && prep.meta.TakenAt.Year() > 1 {
		takenAt = prep.meta.TakenAt
	}
	relPath := storage.OriginalRelPath(takenAt, prep.filename)
	if p.store.OriginalExists(relPath) {
		relPath = appendHashSuffix(relPath, prep.fileHash)
	}
	return relPath
}

// persistPhoto inserts the photo row plus its primary photo_files row.
// On a photo_files failure the photo row is rolled back so the database
// never points at a path that the deferred rollback in Ingest will erase.
func (p *Pipeline) persistPhoto(
	ctx context.Context,
	relPath string,
	prep *ingestPrep,
	opts Options,
) (*database.Photo, error) {
	photoRecord := buildPhoto(opts, relPath, prep.fileHash, prep.fileSize, prep.format, prep.meta)
	if err := p.repo.CreatePhoto(ctx, photoRecord); err != nil {
		return nil, fmt.Errorf("photopipe: create photo row: %w", err)
	}
	primaryFile := &database.PhotoFile{
		PhotoUID:  photoRecord.UID,
		FilePath:  relPath,
		FileHash:  prep.fileHash,
		FileSize:  prep.fileSize,
		FileMime:  photoRecord.FileMime,
		IsPrimary: true,
		Role:      "original",
	}
	if err := p.repo.AddPhotoFile(ctx, primaryFile); err != nil {
		if delErr := p.repo.DeletePhoto(ctx, photoRecord.UID); delErr != nil {
			log.Printf("photopipe: rollback delete photo %q: %v", photoRecord.UID, delErr)
		}
		return nil, fmt.Errorf("photopipe: add photo file: %w", err)
	}
	return photoRecord, nil
}

// generateThumbsBestEffort runs thumb.GenerateAll when opts.GenerateThumbs
// is set. Failures are logged and swallowed — the photo row is already
// persisted and the thumbnails can be regenerated by an out-of-band job.
func (p *Pipeline) generateThumbsBestEffort(prep *ingestPrep, photoRecord *database.Photo, opts Options) {
	if !opts.GenerateThumbs {
		return
	}
	source := thumb.Source{Path: prep.decodable, Orientation: prep.meta.Orientation}
	if _, err := thumb.GenerateAll(source, p.store, prep.fileHash); err != nil {
		log.Printf("photopipe: generate thumbnails for %q: %v", photoRecord.UID, err)
	}
}

// bufferAndHash streams src into a freshly created temp file under the OS
// temp dir, computing the SHA256 hash and byte count on the way. The
// returned path must be cleaned up by the caller (defer os.Remove).
func bufferAndHash(src io.Reader, displayName string) (string, string, int64, error) {
	// Keep the extension on the temp file so DetectFormat's extension lookup
	// works. CreateTemp accepts the suffix via the pattern's trailing "*".
	pattern := "photopipe-*"
	if ext := filepath.Ext(displayName); ext != "" {
		pattern = "photopipe-*" + ext
	}
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", "", 0, fmt.Errorf("photopipe: create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), src)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", "", 0, fmt.Errorf("photopipe: buffer upload: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", "", 0, fmt.Errorf("photopipe: close temp file: %w", closeErr)
	}
	return tmpPath, hex.EncodeToString(h.Sum(nil)), n, nil
}

// writeOriginalFromTemp opens the buffered temp file and streams it into the
// originals tree via the storage helper. The temp file remains in place so
// later steps (thumbnail decode) can still read it.
func (p *Pipeline) writeOriginalFromTemp(tmpPath, relPath string) error {
	// #nosec G304 -- tmpPath comes from os.CreateTemp in this package.
	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, _, err := p.store.WriteOriginal(relPath, f); err != nil {
		return fmt.Errorf("write original: %w", err)
	}
	return nil
}

// extractMetadata reads EXIF from the buffered temp file. We open the file
// and hand it to ExtractFromReader so the filename-date heuristic in the
// exif package compares against opts.Filename (the user-supplied name)
// rather than the random temp-file basename.
func extractMetadata(ctx context.Context, tmpPath, filename string) (*exif.Metadata, error) {
	// #nosec G304 -- tmpPath was created by os.CreateTemp in this package.
	f, err := os.Open(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("open temp file: %w", err)
	}
	defer func() { _ = f.Close() }()
	meta, err := exif.ExtractFromReader(ctx, f, filename)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	return meta, nil
}

// appendHashSuffix inserts a short-hash disambiguator between the file stem
// and its extension. Used to resolve filename + date collisions in the
// originals tree (e.g. two cameras both producing IMG_001.jpg on the same
// day). The suffix is 8 hex chars — long enough to be unique in practice,
// short enough to keep paths readable.
func appendHashSuffix(relPath, fileHash string) string {
	const suffixLen = 8
	if len(fileHash) < suffixLen {
		// Should never happen — SHA256 hex is 64 chars — but degrade
		// gracefully rather than panic.
		return relPath + "-" + fileHash
	}
	dir, base := path.Split(relPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return dir + stem + "-" + fileHash[:suffixLen] + ext
}

// buildPhoto fills out a *database.Photo from the metadata gathered during
// the pipeline. The UID is left empty so the repository can mint one.
func buildPhoto(
	opts Options,
	relPath, fileHash string,
	fileSize int64,
	format string,
	meta *exif.Metadata,
) *database.Photo {
	mime := meta.Mime
	if mime == "" {
		mime = mimeFromFormat(format)
	}

	var takenAt *time.Time
	if !meta.TakenAt.IsZero() {
		t := meta.TakenAt
		takenAt = &t
	}

	source := meta.TakenAtSource
	if source == "" {
		source = "unknown"
	}

	return &database.Photo{
		FileHash:        fileHash,
		FilePath:        relPath,
		FileName:        filepath.Base(opts.Filename),
		FileSize:        fileSize,
		FileMime:        mime,
		FileWidth:       meta.Width,
		FileHeight:      meta.Height,
		FileOrientation: meta.Orientation,
		TakenAt:         takenAt,
		TakenAtSource:   source,
		Lat:             meta.Lat,
		Lng:             meta.Lng,
		Altitude:        meta.Altitude,
		CameraMake:      meta.CameraMake,
		CameraModel:     meta.CameraModel,
		LensModel:       meta.LensModel,
		ISO:             meta.ISO,
		Aperture:        meta.Aperture,
		Exposure:        meta.Exposure,
		FocalLength:     meta.FocalLength,
		Exif:            meta.Raw,
		UploadedBy:      opts.UploadedBy,
	}
}

// mimeFromFormat is a last-resort MIME mapping used when EXIF parsing
// didn't populate the field (rare but possible for malformed JPEGs).
func mimeFromFormat(format string) string {
	switch format {
	case imgconvert.FormatJPEG:
		return "image/jpeg"
	case imgconvert.FormatPNG:
		return "image/png"
	case imgconvert.FormatWebP:
		return "image/webp"
	case imgconvert.FormatHEIC:
		return "image/heic"
	case imgconvert.FormatRAW:
		return "image/x-raw"
	default:
		return "application/octet-stream"
	}
}
