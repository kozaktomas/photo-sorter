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
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/imgconvert"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
)

// Default thresholds for the near-duplicate detector. These match the
// values documented in docs/specs and may be overridden via the
// DuplicateDetection options on a per-pipeline basis.
const (
	// DefaultPHashMaxDiff is the maximum hamming distance (0–64) between
	// two 64-bit pHashes at which they are considered near-duplicates.
	DefaultPHashMaxDiff = 8

	// DefaultEmbeddingMaxDistance is the maximum cosine distance between
	// two CLIP embeddings at which they are considered near-duplicates.
	DefaultEmbeddingMaxDistance = 0.05

	// nearDuplicateEmbeddingFetchLimit is the upper bound on how many
	// candidate matches the embedding-based check pulls back from
	// pgvector's HNSW index per upload. Eight is plenty — the UI only
	// surfaces the closest few — and keeps memory + JSON-payload bounded.
	nearDuplicateEmbeddingFetchLimit = 8
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
	// CheckNearDuplicates controls whether the perceptual-hash + embedding
	// near-duplicate detector runs before the photo row is inserted. When
	// matches are found they are returned on IngestResult.NearDuplicates
	// for the caller to surface in the UI; ingestion still proceeds (the
	// user decides whether to keep the new file). Default false — the
	// HTTP upload handler sets this to true; migration code keeps it off.
	CheckNearDuplicates bool
	// Embedding is the optional pre-computed CLIP embedding for the photo
	// being uploaded. When set together with CheckNearDuplicates and a
	// non-nil EmbeddingReader on the pipeline, it is queried against the
	// pgvector image-embedding index to find visually-similar photos. The
	// pHash check runs regardless of whether the embedding is supplied.
	Embedding []float32
}

// DuplicateMatch is one near-duplicate hit reported on IngestResult. The
// pHash score is the hamming distance between the candidate and the
// matched photo's pHash (0 = bit-identical, 64 = bit-opposite). The
// embedding score is 1 - cosine_distance — i.e. 1.0 when the embeddings
// are bit-identical, 0.0 when orthogonal. Either score may be the zero
// value to indicate "not evaluated" (e.g. score_embedding == 0 when the
// embedding check was skipped because no embedding was supplied).
type DuplicateMatch struct {
	PhotoUID       string     `json:"photo_uid"`
	FileName       string     `json:"file_name"`
	TakenAt        *time.Time `json:"taken_at,omitempty"`
	ScorePHash     int        `json:"score_phash"`
	ScoreEmbedding float64    `json:"score_embedding"`
}

// IngestResult bundles the persisted photo with any near-duplicate matches
// the pipeline found before writing. The pipeline always returns a non-nil
// result on success (even when NearDuplicates is empty); a nil result is
// returned together with an error.
type IngestResult struct {
	Photo          *database.Photo
	NearDuplicates []DuplicateMatch
}

// Pipeline owns the storage layer, the photo repository (write side), and a
// read-side handle for the duplicate check. The reader is broken out from
// the writer so future code paths can hand in a read replica or a cache.
//
// phashStore and embeddingReader are optional — when both are nil the
// near-duplicate detector is disabled even if Options.CheckNearDuplicates
// is true. duplicateOpts overrides the package-level threshold constants;
// the zero value falls back to the defaults.
type Pipeline struct {
	store           *storage.Storage
	repo            database.PhotoWriter
	reader          database.PhotoReader
	phashStore      database.PHashWriter
	embeddingReader database.EmbeddingReader
	duplicateOpts   DuplicateDetectionOptions
}

// DuplicateDetectionOptions are the runtime-tunable thresholds for the
// near-duplicate detector. A zero value of either field is treated as
// "use the package default" (DefaultPHashMaxDiff, DefaultEmbeddingMaxDistance).
// Enabled gates the entire detector — set false to disable globally even
// when the per-ingest Options.CheckNearDuplicates is true.
type DuplicateDetectionOptions struct {
	Enabled              bool
	PHashMaxDiff         int
	EmbeddingMaxDistance float64
}

// effectivePHashMaxDiff returns the configured threshold or the package
// default when the configured value is zero.
func (o DuplicateDetectionOptions) effectivePHashMaxDiff() int {
	if o.PHashMaxDiff <= 0 {
		return DefaultPHashMaxDiff
	}
	return o.PHashMaxDiff
}

// effectiveEmbeddingMaxDistance returns the configured threshold or the
// package default when the configured value is zero.
func (o DuplicateDetectionOptions) effectiveEmbeddingMaxDistance() float64 {
	if o.EmbeddingMaxDistance <= 0 {
		return DefaultEmbeddingMaxDistance
	}
	return o.EmbeddingMaxDistance
}

// New constructs a Pipeline with the duplicate detector disabled. All three
// dependencies are required; the function panics on nil arguments because
// misconfiguration here is a programmer error, not a runtime condition the
// pipeline can recover from. Callers that need the near-duplicate detector
// should use NewWithDuplicateDetection instead.
func New(store *storage.Storage, repo database.PhotoWriter, reader database.PhotoReader) *Pipeline {
	return NewWithDuplicateDetection(store, repo, reader, nil, nil, DuplicateDetectionOptions{})
}

// NewWithDuplicateDetection constructs a Pipeline with optional near-
// duplicate detection. phashStore and embeddingReader may be nil — when
// both are nil the detector is disabled even if Options.CheckNearDuplicates
// is true on an individual call. duplicateOpts.Enabled = false also
// disables the detector globally.
func NewWithDuplicateDetection(
	store *storage.Storage,
	repo database.PhotoWriter,
	reader database.PhotoReader,
	phashStore database.PHashWriter,
	embeddingReader database.EmbeddingReader,
	duplicateOpts DuplicateDetectionOptions,
) *Pipeline {
	if store == nil {
		panic("photopipe: store must not be nil")
	}
	if repo == nil {
		panic("photopipe: repo must not be nil")
	}
	if reader == nil {
		panic("photopipe: reader must not be nil")
	}
	return &Pipeline{
		store:           store,
		repo:            repo,
		reader:          reader,
		phashStore:      phashStore,
		embeddingReader: embeddingReader,
		duplicateOpts:   duplicateOpts,
	}
}

// ingestPrep bundles the intermediate state produced by the read-only
// portion of the pipeline (buffering, format detection, duplicate check,
// decoding, EXIF extraction, near-duplicate scan). Passing it as a struct
// keeps Ingest's body short enough for gocognit and makes the rollback
// path easy to reason about — every field is set before any side effect
// on disk or in the DB.
type ingestPrep struct {
	tmpPath        string
	decodable      string
	filename       string
	fileHash       string
	fileSize       int64
	format         string
	meta           *exif.Metadata
	existing       *database.Photo
	phashBits      uint64
	dhashBits      uint64
	phashComputed  bool
	nearDuplicates []DuplicateMatch
}

// Ingest runs the full upload pipeline on src. On success the returned
// *IngestResult.Photo is the persisted photo row and NearDuplicates is the
// (possibly empty) set of near-duplicate matches found before the write.
// Callers that hit an exact-hash duplicate get back the existing photo on
// the *DuplicateError unwrap path; near duplicates are non-fatal and are
// surfaced alongside the new photo row on the success path.
func (p *Pipeline) Ingest(ctx context.Context, src io.Reader, opts Options) (*IngestResult, error) {
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
		return &IngestResult{Photo: prep.existing}, &DuplicateError{Existing: prep.existing}
	}

	relPath := p.resolveOriginalPath(prep)
	if err := p.writeOriginalFromTemp(prep.tmpPath, relPath); err != nil {
		return nil, fmt.Errorf("photopipe: write original %q: %w", relPath, err)
	}

	// From here on the original is on disk. If anything below fails the
	// deferred rollback removes it so we don't leave orphaned files behind.
	//
	// Race-safety detail: two concurrent uploads with the same SHA256 may
	// both observe "no duplicate" in lookupDuplicate, both pick the same
	// relPath (resolveOriginalPath checks OriginalExists which is itself
	// racey), and both call WriteOriginal. The atomic rename means the
	// resulting file is well-formed and identical-content for both, but
	// only one CreatePhoto will succeed (UNIQUE on photos.file_hash).
	// The losing transaction's rollback MUST NOT delete the file: the
	// winner's photo row points at it. p.releaseOriginalIfOurs makes the
	// post-hoc ownership check before unlinking.
	success := false
	defer func() {
		if success {
			return
		}
		p.releaseOriginalIfOurs(ctx, relPath, prep.fileHash)
	}()

	photoRecord, err := p.persistPhoto(ctx, relPath, prep, opts)
	if err != nil {
		return nil, err
	}

	p.generateThumbsBestEffort(prep, photoRecord, opts)
	p.persistPHashBestEffort(ctx, prep, photoRecord)

	success = true
	return &IngestResult{Photo: photoRecord, NearDuplicates: prep.nearDuplicates}, nil
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

	prep := &ingestPrep{
		tmpPath:   tmpPath,
		decodable: decodable,
		filename:  opts.Filename,
		fileHash:  fileHash,
		fileSize:  fileSize,
		format:    format,
		meta:      meta,
	}

	if p.duplicateCheckEnabled(opts) {
		if err := p.populateNearDuplicates(ctx, prep, opts); err != nil {
			// Near-duplicate detection is best-effort: a database hiccup
			// or a malformed decodable here must not fail the upload. We
			// log it and continue with prep.nearDuplicates == nil so the
			// caller still gets the photo row.
			log.Printf("photopipe: near-duplicate scan for %q: %v", opts.Filename, err)
		}
	}

	return prep, combined, nil
}

// duplicateCheckEnabled returns true when the per-call opt-in is set, the
// pipeline-level switch is on, and at least one of the underlying stores
// (phashStore or embeddingReader) is wired up. The check needs at least
// one signal source to do anything useful — both nil means "no-op".
func (p *Pipeline) duplicateCheckEnabled(opts Options) bool {
	if !opts.CheckNearDuplicates {
		return false
	}
	if !p.duplicateOpts.Enabled {
		return false
	}
	return p.phashStore != nil || p.embeddingReader != nil
}

// populateNearDuplicates computes the candidate pHash and fills
// prep.nearDuplicates with whatever matches the two index sources produce.
// Errors from either source are returned to the caller, which logs them
// (the upload still proceeds). The set of matches is de-duplicated by
// photo_uid so a photo flagged by both detectors only appears once.
func (p *Pipeline) populateNearDuplicates(ctx context.Context, prep *ingestPrep, opts Options) error {
	if prep.decodable == "" {
		return nil
	}

	// Compute pHash from the decodable JPEG-friendly intermediate so HEIC
	// and RAW uploads also feed the detector. Failures here are non-fatal —
	// we simply skip the pHash branch and let the embedding branch try.
	if p.phashStore != nil {
		decoded, err := os.ReadFile(prep.decodable) // #nosec G304 -- decodable comes from imgconvert in this package
		if err != nil {
			return fmt.Errorf("read decodable: %w", err)
		}
		hashes, err := fingerprint.ComputeHashes(decoded)
		if err != nil {
			return fmt.Errorf("compute phashes: %w", err)
		}
		prep.phashBits = hashes.PHashBits
		prep.dhashBits = hashes.DHashBits
		prep.phashComputed = true
	}

	matches := make(map[string]DuplicateMatch)
	if prep.phashComputed {
		if err := p.scanPHashMatches(ctx, prep.phashBits, matches); err != nil {
			return fmt.Errorf("phash scan: %w", err)
		}
	}
	if p.embeddingReader != nil && len(opts.Embedding) > 0 {
		if err := p.scanEmbeddingMatches(ctx, opts.Embedding, matches); err != nil {
			return fmt.Errorf("embedding scan: %w", err)
		}
	}

	prep.nearDuplicates = sortDuplicateMatches(matches)
	return nil
}

// scanPHashMatches fetches every row from photo_phashes and inserts any
// entry within p.duplicateOpts.effectivePHashMaxDiff() bits into matches.
// The map is keyed by photo_uid so a later embedding hit on the same photo
// overlays the pHash score instead of duplicating the entry.
func (p *Pipeline) scanPHashMatches(
	ctx context.Context, candidate uint64, matches map[string]DuplicateMatch,
) error {
	rows, err := p.phashStore.ListAllPHashes(ctx)
	if err != nil {
		return fmt.Errorf("list phashes: %w", err)
	}
	threshold := p.duplicateOpts.effectivePHashMaxDiff()
	for _, row := range rows {
		dist := fingerprint.HammingDistance(candidate, row.PHash)
		if dist > threshold {
			continue
		}
		match, err := p.buildMatch(ctx, row.PhotoUID)
		if err != nil {
			// Missing photo row should not happen (cascade FK) but skip
			// the match rather than failing the whole scan.
			continue
		}
		match.ScorePHash = dist
		matches[row.PhotoUID] = match
	}
	return nil
}

// scanEmbeddingMatches queries the pgvector embedding HNSW index for the
// candidate embedding and overlays any hits onto matches. The score is
// reported as 1 - cosine_distance so the UI can present a 0..1
// "similarity" value where 1 means identical.
func (p *Pipeline) scanEmbeddingMatches(
	ctx context.Context, embedding []float32, matches map[string]DuplicateMatch,
) error {
	maxDist := p.duplicateOpts.effectiveEmbeddingMaxDistance()
	hits, distances, err := p.embeddingReader.FindSimilarWithDistance(
		ctx, embedding, nearDuplicateEmbeddingFetchLimit, maxDist,
	)
	if err != nil {
		return fmt.Errorf("find similar embeddings: %w", err)
	}
	for i, hit := range hits {
		match, ok := matches[hit.PhotoUID]
		if !ok {
			built, err := p.buildMatch(ctx, hit.PhotoUID)
			if err != nil {
				continue
			}
			match = built
		}
		match.ScoreEmbedding = 1.0 - distances[i]
		matches[hit.PhotoUID] = match
	}
	return nil
}

// buildMatch fills in the descriptive fields (filename + taken_at) by
// looking the photo up by UID. Returns an error when the row is missing
// or the reader fails so the scanner can skip the match.
func (p *Pipeline) buildMatch(ctx context.Context, photoUID string) (DuplicateMatch, error) {
	photo, err := p.reader.GetPhoto(ctx, photoUID)
	if err != nil {
		return DuplicateMatch{}, fmt.Errorf("get photo %q: %w", photoUID, err)
	}
	return DuplicateMatch{
		PhotoUID: photo.UID,
		FileName: photo.FileName,
		TakenAt:  photo.TakenAt,
	}, nil
}

// sortDuplicateMatches converts the dedup map into a slice ordered by
// "most likely a duplicate first": embedding score descending (higher
// similarity wins), then pHash distance ascending (fewer differing bits
// wins). The result is deterministic for tests.
func sortDuplicateMatches(matches map[string]DuplicateMatch) []DuplicateMatch {
	out := make([]DuplicateMatch, 0, len(matches))
	for _, m := range matches {
		out = append(out, m)
	}
	// Simple insertion sort — N is small (capped at index fetch limit + pHash matches).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && lessLikelyDuplicate(out[j-1], out[j]); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// lessLikelyDuplicate returns true when a is less likely to be a duplicate
// than b — i.e. b should sort before a. Used as the comparator for
// sortDuplicateMatches.
func lessLikelyDuplicate(a, b DuplicateMatch) bool {
	if a.ScoreEmbedding != b.ScoreEmbedding {
		return a.ScoreEmbedding < b.ScoreEmbedding
	}
	return a.ScorePHash > b.ScorePHash
}

// persistPHashBestEffort writes the computed pHash + dHash for the new
// photo into photo_phashes. Failures are logged and swallowed — the photo
// row is already persisted; a missed pHash row just means the duplicate
// detector will skip this photo on future uploads. The backfill CLI can
// fix it later.
func (p *Pipeline) persistPHashBestEffort(
	ctx context.Context, prep *ingestPrep, photoRecord *database.Photo,
) {
	if p.phashStore == nil || !prep.phashComputed {
		return
	}
	if err := p.phashStore.SavePHash(ctx, photoRecord.UID, prep.phashBits, prep.dhashBits); err != nil {
		log.Printf("photopipe: persist phash for %q: %v", photoRecord.UID, err)
	}
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

// releaseOriginalIfOurs deletes the on-disk original at relPath UNLESS a
// concurrent upload with the same SHA256 has already won the
// CreatePhoto race and its row points at the same path. The check
// closes a narrow window in which two uploaders both observed "not a
// duplicate", both wrote (rename-overwrites means the bytes are the
// same), and the losing CreatePhoto came back with a unique-violation;
// without the ownership probe the losing rollback would unlink the
// winner's file and corrupt the catalogue.
func (p *Pipeline) releaseOriginalIfOurs(ctx context.Context, relPath, fileHash string) {
	winner, err := p.reader.GetPhotoByHash(ctx, fileHash)
	if err == nil && winner != nil && winner.FilePath == relPath {
		// Another concurrent upload owns this file now. Leave it alone.
		return
	}
	if delErr := p.store.DeleteOriginal(relPath); delErr != nil {
		log.Printf("photopipe: rollback delete original %q: %v", relPath, delErr)
	}
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
