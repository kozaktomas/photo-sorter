package photopipe

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// fakePhotoStore is a minimal in-memory implementation of database.PhotoWriter
// covering exactly the methods Pipeline.Ingest exercises. Anything not
// touched by the duplicate-detection tests panics, so accidental
// dependencies on other methods surface immediately.
type fakePhotoStore struct {
	mu       sync.Mutex
	photos   map[string]*database.Photo
	byHash   map[string]string
	files    map[string][]database.PhotoFile
	createID int
}

func newFakePhotoStore() *fakePhotoStore {
	return &fakePhotoStore{
		photos: make(map[string]*database.Photo),
		byHash: make(map[string]string),
		files:  make(map[string][]database.PhotoFile),
	}
}

func (s *fakePhotoStore) GetPhoto(_ context.Context, uid string) (*database.Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.photos[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	clone := *p
	return &clone, nil
}

func (s *fakePhotoStore) GetPhotoByHash(_ context.Context, hash string) (*database.Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, ok := s.byHash[hash]
	if !ok {
		return nil, database.ErrNotFound
	}
	clone := *s.photos[uid]
	return &clone, nil
}

func (s *fakePhotoStore) ListPhotos(
	_ context.Context, _ database.PhotoFilter,
) ([]database.Photo, int, error) {
	panic("not implemented")
}

func (s *fakePhotoStore) ListPhotoFiles(
	_ context.Context, uid string,
) ([]database.PhotoFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]database.PhotoFile(nil), s.files[uid]...), nil
}

func (s *fakePhotoStore) ListArchivedBefore(
	_ context.Context, _ time.Time,
) ([]string, error) {
	panic("not implemented")
}

func (s *fakePhotoStore) CreatePhoto(_ context.Context, p *database.Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.UID == "" {
		s.createID++
		p.UID = "pfake" + string(rune('a'+s.createID))
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	clone := *p
	s.photos[p.UID] = &clone
	s.byHash[p.FileHash] = p.UID
	return nil
}

func (s *fakePhotoStore) UpdatePhoto(_ context.Context, _ *database.Photo) error {
	panic("not implemented")
}

func (s *fakePhotoStore) DeletePhoto(_ context.Context, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.photos[uid]
	if !ok {
		return database.ErrNotFound
	}
	delete(s.photos, uid)
	delete(s.byHash, p.FileHash)
	delete(s.files, uid)
	return nil
}

func (s *fakePhotoStore) ArchivePhoto(_ context.Context, _ string) error {
	panic("not implemented")
}

func (s *fakePhotoStore) RestorePhoto(_ context.Context, _ string) error {
	panic("not implemented")
}

func (s *fakePhotoStore) AddPhotoFile(_ context.Context, f *database.PhotoFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[f.PhotoUID] = append(s.files[f.PhotoUID], *f)
	return nil
}

func (s *fakePhotoStore) DeletePhotoFile(_ context.Context, _, _ string) error {
	panic("not implemented")
}

// fakePHashStore is an in-memory implementation of database.PHashWriter.
type fakePHashStore struct {
	mu   sync.Mutex
	rows map[string]database.PhotoPHash
}

func newFakePHashStore() *fakePHashStore {
	return &fakePHashStore{rows: make(map[string]database.PhotoPHash)}
}

func (s *fakePHashStore) GetPHash(_ context.Context, uid string) (*database.PhotoPHash, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	clone := row
	return &clone, nil
}

func (s *fakePHashStore) ListAllPHashes(_ context.Context) ([]database.PhotoPHash, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]database.PhotoPHash, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *fakePHashStore) CountPHashes(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows), nil
}

func (s *fakePHashStore) ListPhotosWithoutPHash(_ context.Context, _ int) ([]string, error) {
	return nil, nil
}

func (s *fakePHashStore) SavePHash(_ context.Context, uid string, ph, dh uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[uid] = database.PhotoPHash{PhotoUID: uid, PHash: ph, DHash: dh, CreatedAt: time.Now().UTC()}
	return nil
}

func (s *fakePHashStore) DeletePHash(_ context.Context, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, uid)
	return nil
}

// fakeEmbeddingReader is the minimum embedding reader needed to exercise
// the embedding branch of the duplicate detector. Only FindSimilarWithDistance
// is implemented; the other methods panic so an accidental dependency is
// loud.
type fakeEmbeddingReader struct {
	mu    sync.Mutex
	rows  []database.StoredEmbedding
	calls int
}

func (r *fakeEmbeddingReader) Get(_ context.Context, _ string) (*database.StoredEmbedding, error) {
	panic("not implemented")
}

func (r *fakeEmbeddingReader) Has(_ context.Context, _ string) (bool, error) {
	panic("not implemented")
}

func (r *fakeEmbeddingReader) Count(_ context.Context) (int, error) { panic("not implemented") }
func (r *fakeEmbeddingReader) CountByUIDs(_ context.Context, _ []string) (int, error) {
	panic("not implemented")
}

func (r *fakeEmbeddingReader) FindSimilar(
	_ context.Context, _ []float32, _ int,
) ([]database.StoredEmbedding, error) {
	panic("not implemented")
}

func (r *fakeEmbeddingReader) FindSimilarWithDistance(
	_ context.Context, query []float32, limit int, maxDistance float64,
) ([]database.StoredEmbedding, []float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++

	var (
		matches   []database.StoredEmbedding
		distances []float64
	)
	for _, row := range r.rows {
		dist := 1.0 - float64(fingerprint.CosineSimilarity(query, row.Embedding))
		if dist > maxDistance {
			continue
		}
		matches = append(matches, row)
		distances = append(distances, dist)
		if len(matches) >= limit {
			break
		}
	}
	return matches, distances, nil
}

func (r *fakeEmbeddingReader) GetUniquePhotoUIDs(_ context.Context) ([]string, error) {
	panic("not implemented")
}

func (r *fakeEmbeddingReader) GetCentroid(_ context.Context, _ []string) ([]float32, error) {
	panic("not implemented")
}

// gradientJPEG builds a deterministic gradient image and returns its JPEG
// bytes. seed lets callers create visually-similar but byte-different
// inputs (a re-save at a different quality bumps a handful of pHash bits
// for sufficiently-large images). 256×192 is large enough that lossy
// re-encoding at Q=60 vs Q=95 preserves the pHash to within 0–3 bits.
func gradientJPEG(t *testing.T, width, height int, seed byte, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x%256) ^ seed,
				G: uint8(y%256) ^ seed,
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// stripedJPEG builds a vertical-stripe image and returns its JPEG bytes.
// Used as the "visually unrelated" fixture in TestIngest_UnrelatedPhotosDoNotMatch:
// a stripe pattern has a fundamentally different DCT signature than the
// linear gradient produced by gradientJPEG, so the pHash distance lands
// well above the 8-bit threshold.
func stripedJPEG(t *testing.T, width, height int, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			c := color.RGBA{0, 0, 0, 255}
			if (x/8)%2 == 0 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// newTestStorage builds a Storage rooted at t.TempDir for the duplicate
// detection tests.
func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	root := t.TempDir()
	s, err := storage.New(filepath.Join(root, "originals"), filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s
}

// TestIngest_NearDuplicatePHashMatch reproduces the spec test "Upload two
// visually identical JPEGs (different file hashes) → second upload
// reports a near-duplicate match against the first". The two images
// differ only in JPEG quality, which leaves the pHash within a handful of
// bits of the original.
func TestIngest_NearDuplicatePHashMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newTestStorage(t)
	photoStore := newFakePhotoStore()
	phashStore := newFakePHashStore()

	pipeline := NewWithDuplicateDetection(
		store, photoStore, photoStore,
		phashStore, nil,
		DuplicateDetectionOptions{Enabled: true},
	)

	// First upload: high-quality JPEG. 256×192 is the smallest size at
	// which JPEG re-encoding preserves the pHash well — at 96×64 the
	// quantisation noise dominates the low-frequency coefficients pHash
	// hashes and the test becomes flaky.
	original := gradientJPEG(t, 256, 192, 0x11, 95)
	first, err := pipeline.Ingest(ctx, bytes.NewReader(original), Options{
		Filename:            "first.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
	})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if len(first.NearDuplicates) != 0 {
		t.Errorf("first upload should not have near-duplicates, got %d", len(first.NearDuplicates))
	}

	// Verify the pHash row was actually persisted.
	row, err := phashStore.GetPHash(ctx, first.Photo.UID)
	if err != nil {
		t.Fatalf("expected phash row for first upload: %v", err)
	}
	if row.PHash == 0 && row.DHash == 0 {
		t.Fatal("phash row was persisted with zero hashes")
	}

	// Second upload: same source image re-encoded at lower quality. The
	// SHA256 differs (so the exact-hash dedup misses) but the pHash should
	// still be within the threshold.
	reencoded := gradientJPEG(t, 256, 192, 0x11, 60)
	if bytes.Equal(original, reencoded) {
		t.Fatal("test setup: re-encode produced byte-identical output")
	}
	second, err := pipeline.Ingest(ctx, bytes.NewReader(reencoded), Options{
		Filename:            "second.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
	})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	if len(second.NearDuplicates) == 0 {
		t.Fatalf("second upload should have at least one near-duplicate match")
	}

	match := second.NearDuplicates[0]
	if match.PhotoUID != first.Photo.UID {
		t.Errorf("match.PhotoUID = %q, want %q", match.PhotoUID, first.Photo.UID)
	}
	if match.FileName != first.Photo.FileName {
		t.Errorf("match.FileName = %q, want %q", match.FileName, first.Photo.FileName)
	}
	if match.ScorePHash < 0 || match.ScorePHash > DefaultPHashMaxDiff {
		t.Errorf("match.ScorePHash = %d, want 0..%d", match.ScorePHash, DefaultPHashMaxDiff)
	}
}

// TestIngest_NearDuplicateDisabled covers the spec test "Disable the check
// via config → no match reported". Setting Enabled=false on the pipeline-
// level DuplicateDetectionOptions short-circuits the detector even when
// the per-call CheckNearDuplicates flag is true.
func TestIngest_NearDuplicateDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newTestStorage(t)
	photoStore := newFakePhotoStore()
	phashStore := newFakePHashStore()

	pipeline := NewWithDuplicateDetection(
		store, photoStore, photoStore,
		phashStore, nil,
		DuplicateDetectionOptions{Enabled: false},
	)

	first := gradientJPEG(t, 256, 192, 0x22, 95)
	if _, err := pipeline.Ingest(ctx, bytes.NewReader(first), Options{
		Filename:            "first.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
	}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	second := gradientJPEG(t, 256, 192, 0x22, 60)
	res, err := pipeline.Ingest(ctx, bytes.NewReader(second), Options{
		Filename:            "second.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
	})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if len(res.NearDuplicates) != 0 {
		t.Errorf("near-duplicate scan should be disabled; got %d matches", len(res.NearDuplicates))
	}
	if _, err := phashStore.GetPHash(ctx, res.Photo.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("phash row should NOT be persisted when detector is disabled; got err=%v", err)
	}
}

// TestIngest_UnrelatedPhotosDoNotMatch covers the spec test "pHash
// distance for unrelated photos is well above the threshold". Two
// completely different gradient images should not be flagged as
// near-duplicates.
func TestIngest_UnrelatedPhotosDoNotMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newTestStorage(t)
	photoStore := newFakePhotoStore()
	phashStore := newFakePHashStore()

	pipeline := NewWithDuplicateDetection(
		store, photoStore, photoStore,
		phashStore, nil,
		DuplicateDetectionOptions{Enabled: true},
	)

	// Use a gradient + a striped pattern as the two "unrelated" fixtures.
	// Two different gradients share too much low-frequency energy to land
	// reliably above the 8-bit pHash threshold; a stripe pattern has a
	// fundamentally different DCT signature so distance is comfortably 20+.
	first := gradientJPEG(t, 256, 192, 0x33, 92)
	if _, err := pipeline.Ingest(ctx, bytes.NewReader(first), Options{
		Filename:            "alpha.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
	}); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	second := stripedJPEG(t, 256, 192, 92)
	res, err := pipeline.Ingest(ctx, bytes.NewReader(second), Options{
		Filename:            "beta.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
	})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}

	if len(res.NearDuplicates) != 0 {
		t.Errorf("unrelated images should not match; got %d matches: %+v",
			len(res.NearDuplicates), res.NearDuplicates)
	}
}

// TestIngest_NearDuplicateEmbeddingMatch verifies the embedding branch of
// the detector lights up when a sufficiently-similar embedding is
// supplied. The pHash branch is exercised by the tests above; this one
// proves the two scoring sources combine into a single match entry.
func TestIngest_NearDuplicateEmbeddingMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newTestStorage(t)
	photoStore := newFakePhotoStore()
	phashStore := newFakePHashStore()

	knownEmbedding := []float32{1, 0, 0, 0}
	embReader := &fakeEmbeddingReader{
		rows: []database.StoredEmbedding{
			{PhotoUID: "pseed00000000000", Embedding: knownEmbedding, Dim: 4},
		},
	}
	// Seed a photo row so the duplicate scanner can resolve the embedding
	// hit into a DuplicateMatch (the embedding scan calls GetPhoto for the
	// matched UID).
	if err := photoStore.CreatePhoto(ctx, &database.Photo{
		UID: "pseed00000000000", FileHash: "seed-hash",
		FilePath: "unknown/seed.jpg", FileName: "seed.jpg",
	}); err != nil {
		t.Fatalf("seed photo: %v", err)
	}

	pipeline := NewWithDuplicateDetection(
		store, photoStore, photoStore,
		phashStore, embReader,
		DuplicateDetectionOptions{Enabled: true},
	)

	candidate := []float32{1, 0, 0, 0}
	data := gradientJPEG(t, 64, 48, 0x55, 90)
	res, err := pipeline.Ingest(ctx, bytes.NewReader(data), Options{
		Filename:            "candidate.jpg",
		GenerateThumbs:      false,
		SkipDuplicates:      true,
		CheckNearDuplicates: true,
		Embedding:           candidate,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if len(res.NearDuplicates) != 1 {
		t.Fatalf("want 1 near-duplicate via embedding match, got %d", len(res.NearDuplicates))
	}
	hit := res.NearDuplicates[0]
	if hit.PhotoUID != "pseed00000000000" {
		t.Errorf("match.PhotoUID = %q, want pseed00000000000", hit.PhotoUID)
	}
	if hit.ScoreEmbedding < 0.99 {
		t.Errorf("match.ScoreEmbedding = %v, want ~1.0", hit.ScoreEmbedding)
	}
	if embReader.calls != 1 {
		t.Errorf("FindSimilarWithDistance call count = %d, want 1", embReader.calls)
	}
}
