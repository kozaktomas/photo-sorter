//go:build integration

package photopipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

// setupPipeline boots a pgvector container, applies migrations, allocates a
// fresh on-disk storage tree under t.TempDir, and returns a wired Pipeline
// together with the underlying repository handle and a cleanup function.
// The cleanup tears the container down; the temp dirs are reaped by the
// test harness.
func setupPipeline(t *testing.T) (*Pipeline, *postgres.PhotoRepository, *storage.Storage, func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: tcwait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("Docker not available or container failed to start: %v", err)
		return nil, nil, nil, func() {}
	}
	if container == nil {
		t.Skip("Docker not available")
		return nil, nil, nil, func() {}
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("get container port: %v", err)
	}
	dbURL := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())

	pool, err := postgres.NewPool(&config.DatabaseConfig{
		URL:          dbURL,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	})
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("create pool: %v", err)
	}
	if err := pool.Migrate(ctx); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("run migrations: %v", err)
	}

	root := t.TempDir()
	store, err := storage.New(filepath.Join(root, "originals"), filepath.Join(root, "cache"))
	if err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("storage.New: %v", err)
	}
	repo := postgres.NewPhotoRepository(pool)
	pipeline := New(store, repo, repo)

	cleanup := func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
	return pipeline, repo, store, cleanup
}

// jpegWithoutEXIF renders a deterministic gradient at width × height and
// JPEG-encodes it into an in-memory buffer. The result carries no EXIF tags
// at all, so the pipeline must fall back to "unknown" for the date source.
func jpegWithoutEXIF(t *testing.T, width, height int, seed byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x%256) ^ seed,
				G: uint8(y%256) ^ seed,
				B: 128,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// readEXIFFixture returns the bytes of the JPEG fixture shipped with the
// exif package, which carries a real DateTimeOriginal tag the pipeline
// must pick up.
func readEXIFFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../exif/testdata/basic.jpg")
	if err != nil {
		t.Fatalf("read exif fixture: %v", err)
	}
	return data
}

// TestIngest_JPEGWithEXIF covers the spec test "Ingest a JPEG with full
// EXIF → record exists, thumbnails generated, hashes match."
func TestIngest_JPEGWithEXIF(t *testing.T) {
	pipeline, repo, store, cleanup := setupPipeline(t)
	if pipeline == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	data := readEXIFFixture(t)
	// Leave UploadedBy empty — the photos.uploaded_by column has a FK to
	// the users table, and the test container doesn't seed users.
	opts := Options{
		Filename:       "IMG_0001.jpg",
		GenerateThumbs: true,
		SkipDuplicates: true,
	}
	got, err := pipeline.Ingest(ctx, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got.UID == "" {
		t.Fatal("UID not populated")
	}
	if got.FileSize != int64(len(data)) {
		t.Errorf("FileSize = %d, want %d", got.FileSize, len(data))
	}
	if got.FileMime != "image/jpeg" {
		t.Errorf("FileMime = %q, want image/jpeg", got.FileMime)
	}
	if got.TakenAt == nil {
		t.Error("TakenAt should be populated from EXIF")
	}
	if got.TakenAtSource != "exif" {
		t.Errorf("TakenAtSource = %q, want exif", got.TakenAtSource)
	}

	// The on-disk relative path must encode YYYY/MM/<sanitized filename>.
	if got.TakenAt != nil {
		wantPrefix := fmt.Sprintf("%04d/%02d/", got.TakenAt.Year(), int(got.TakenAt.Month()))
		if !strings.HasPrefix(got.FilePath, wantPrefix) {
			t.Errorf("FilePath = %q, want prefix %q", got.FilePath, wantPrefix)
		}
	}

	// Re-read the photo from the database to confirm persistence.
	persisted, err := repo.GetPhoto(ctx, got.UID)
	if err != nil {
		t.Fatalf("GetPhoto: %v", err)
	}
	if persisted.FileHash != got.FileHash {
		t.Errorf("persisted FileHash = %q, want %q", persisted.FileHash, got.FileHash)
	}

	// The primary photo_files row must exist with is_primary = true.
	files, err := repo.ListPhotoFiles(ctx, got.UID)
	if err != nil {
		t.Fatalf("ListPhotoFiles: %v", err)
	}
	if len(files) != 1 || !files[0].IsPrimary {
		t.Errorf("expected 1 primary photo_files row, got %+v", files)
	}

	// The bytes on disk must match what we uploaded.
	abs, err := store.AbsOriginal(got.FilePath)
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	onDisk, err := os.ReadFile(abs) // #nosec G304 -- abs is under storage root
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if !bytes.Equal(onDisk, data) {
		t.Errorf("bytes on disk do not match upload (len=%d vs %d)", len(onDisk), len(data))
	}

	// At least one thumbnail size must have landed in the cache. We check a
	// representative size rather than all ten — the thumb package's own
	// tests already cover the full grid.
	thumbRel, err := storage.ThumbRelPath(got.FileHash, "fit_720")
	if err != nil {
		t.Fatalf("ThumbRelPath: %v", err)
	}
	if !store.ThumbExists(thumbRel) {
		t.Errorf("thumb fit_720 not generated at %q", thumbRel)
	}
}

// TestIngest_DuplicateReturnsExisting covers the spec test "Ingest the same
// JPEG twice → second call returns ErrDuplicate wrapping the first photo;
// no second file on disk."
func TestIngest_DuplicateReturnsExisting(t *testing.T) {
	pipeline, _, store, cleanup := setupPipeline(t)
	if pipeline == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	data := readEXIFFixture(t)
	opts := Options{
		Filename:       "dup.jpg",
		GenerateThumbs: false,
		SkipDuplicates: true,
	}
	first, err := pipeline.Ingest(ctx, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// Snapshot the on-disk file modtime so we can verify the second call
	// did not overwrite or duplicate the original.
	abs, err := store.AbsOriginal(first.FilePath)
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	stat1, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat original: %v", err)
	}

	second, err := pipeline.Ingest(ctx, bytes.NewReader(data), opts)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second ingest: err = %v, want ErrDuplicate", err)
	}
	if second == nil || second.UID != first.UID {
		t.Errorf("duplicate should return existing photo; got %+v want UID=%s", second, first.UID)
	}
	var dupErr *DuplicateError
	if !errors.As(err, &dupErr) {
		t.Fatal("err should unwrap to *DuplicateError")
	}
	if dupErr.Existing == nil || dupErr.Existing.UID != first.UID {
		t.Errorf("DuplicateError.Existing.UID = %v, want %v", dupErr.Existing, first.UID)
	}

	// The on-disk file must not have been rewritten.
	stat2, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat original after dup: %v", err)
	}
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Errorf("original was rewritten on duplicate: %v vs %v", stat1.ModTime(), stat2.ModTime())
	}
	if stat1.Size() != stat2.Size() {
		t.Errorf("original size changed on duplicate: %d vs %d", stat1.Size(), stat2.Size())
	}
}

// TestIngest_NoEXIFGoesUnderUnknown covers the spec test "Ingest a JPEG
// with no EXIF date → falls under unknown/<file> path."
func TestIngest_NoEXIFGoesUnderUnknown(t *testing.T) {
	pipeline, _, store, cleanup := setupPipeline(t)
	if pipeline == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	data := jpegWithoutEXIF(t, 64, 48, 0x11)
	// Use a filename that defeats the filename-date heuristic so we land
	// in the "unknown" branch rather than YYYY/MM via the regex fallback.
	opts := Options{
		Filename:       "no-exif-sample.jpg",
		GenerateThumbs: false,
		SkipDuplicates: true,
	}
	got, err := pipeline.Ingest(ctx, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got.TakenAt != nil {
		t.Errorf("TakenAt should be nil for no-EXIF input, got %v", got.TakenAt)
	}
	if got.TakenAtSource != "unknown" {
		t.Errorf("TakenAtSource = %q, want unknown", got.TakenAtSource)
	}
	if !strings.HasPrefix(got.FilePath, "unknown/") {
		t.Errorf("FilePath = %q, want prefix unknown/", got.FilePath)
	}
	abs, err := store.AbsOriginal(got.FilePath)
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Errorf("original missing on disk at %q: %v", abs, err)
	}
}

// TestIngest_GenerateThumbsFalse covers the spec test "Ingest with
// GenerateThumbs=false → no thumbnails generated."
func TestIngest_GenerateThumbsFalse(t *testing.T) {
	pipeline, _, store, cleanup := setupPipeline(t)
	if pipeline == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	data := jpegWithoutEXIF(t, 64, 48, 0x22)
	opts := Options{
		Filename:       "skip-thumbs.jpg",
		GenerateThumbs: false,
		SkipDuplicates: true,
	}
	got, err := pipeline.Ingest(ctx, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// No thumbnail size — fit_* or tile_* — should have been written.
	checkSizes := []string{
		"fit_720", "fit_1280", "fit_1920", "fit_2560", "fit_3840", "fit_7680",
		"tile_50", "tile_100", "tile_224", "tile_500",
	}
	for _, size := range checkSizes {
		rel, err := storage.ThumbRelPath(got.FileHash, size)
		if err != nil {
			t.Fatalf("ThumbRelPath(%s): %v", size, err)
		}
		if store.ThumbExists(rel) {
			t.Errorf("thumb %q was generated despite GenerateThumbs=false", size)
		}
	}
}

// TestIngest_FilenameCollisionSuffix covers the spec test "Filename
// collision: two different files with the same name and same EXIF date →
// second file gets a -<hash> suffix and both exist on disk."
func TestIngest_FilenameCollisionSuffix(t *testing.T) {
	pipeline, _, store, cleanup := setupPipeline(t)
	if pipeline == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	// Two distinct images (different gradients) with the same filename and
	// no EXIF date. Both land under unknown/ so the directory + base name
	// are identical and the pipeline must disambiguate via -<hash>.
	dataA := jpegWithoutEXIF(t, 64, 48, 0x33)
	dataB := jpegWithoutEXIF(t, 64, 48, 0x44)
	if bytes.Equal(dataA, dataB) {
		t.Fatal("test setup produced identical fixtures; choose different seeds")
	}
	opts := Options{
		Filename:       "clash.jpg",
		GenerateThumbs: false,
		SkipDuplicates: true,
	}
	first, err := pipeline.Ingest(ctx, bytes.NewReader(dataA), opts)
	if err != nil {
		t.Fatalf("ingest A: %v", err)
	}
	second, err := pipeline.Ingest(ctx, bytes.NewReader(dataB), opts)
	if err != nil {
		t.Fatalf("ingest B: %v", err)
	}

	if first.FilePath == second.FilePath {
		t.Errorf("collision not resolved: both paths = %q", first.FilePath)
	}
	if !strings.HasPrefix(second.FilePath, "unknown/") {
		t.Errorf("second path = %q, want prefix unknown/", second.FilePath)
	}
	// The disambiguator is "-<first 8 chars of file hash>" appended to the
	// stem. The clash.jpg stem becomes clash-<hash8>.jpg.
	wantInfix := "-" + second.FileHash[:8] + ".jpg"
	if !strings.HasSuffix(second.FilePath, wantInfix) {
		t.Errorf("second path = %q, want suffix %q", second.FilePath, wantInfix)
	}

	// Both files must exist on disk.
	for _, rel := range []string{first.FilePath, second.FilePath} {
		abs, err := store.AbsOriginal(rel)
		if err != nil {
			t.Fatalf("AbsOriginal(%q): %v", rel, err)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("expected file at %q: %v", abs, err)
		}
	}
}
