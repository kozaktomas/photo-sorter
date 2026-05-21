package trash

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// fakePhotoWriter is the smallest in-memory database.PhotoWriter that the
// trash helpers exercise. Methods PurgePhoto / AutoPurge do not touch
// return all-but-the-archive-related fields, so anything irrelevant
// panics rather than silently returning a zero value.
type fakePhotoWriter struct {
	mu     sync.Mutex
	photos map[string]*database.Photo
	files  map[string][]database.PhotoFile
}

func newFakePhotoWriter() *fakePhotoWriter {
	return &fakePhotoWriter{
		photos: make(map[string]*database.Photo),
		files:  make(map[string][]database.PhotoFile),
	}
}

func (f *fakePhotoWriter) GetPhoto(_ context.Context, uid string) (*database.Photo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.photos[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	clone := *p
	return &clone, nil
}

func (f *fakePhotoWriter) GetPhotoByHash(_ context.Context, _ string) (*database.Photo, error) {
	panic("not implemented")
}

func (f *fakePhotoWriter) ListPhotos(
	_ context.Context, _ database.PhotoFilter,
) ([]database.Photo, int, error) {
	panic("not implemented")
}

func (f *fakePhotoWriter) ListPhotoFiles(
	_ context.Context, uid string,
) ([]database.PhotoFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]database.PhotoFile(nil), f.files[uid]...), nil
}

func (f *fakePhotoWriter) ListArchivedBefore(
	_ context.Context, cutoff time.Time,
) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var uids []string
	for uid, p := range f.photos {
		if p.ArchivedAt != nil && p.ArchivedAt.Before(cutoff) {
			uids = append(uids, uid)
		}
	}
	sort.Strings(uids)
	return uids, nil
}

func (f *fakePhotoWriter) CreatePhoto(_ context.Context, _ *database.Photo) error {
	panic("not implemented")
}

func (f *fakePhotoWriter) UpdatePhoto(_ context.Context, _ *database.Photo) error {
	panic("not implemented")
}

func (f *fakePhotoWriter) DeletePhoto(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.photos[uid]; !ok {
		return database.ErrNotFound
	}
	delete(f.photos, uid)
	delete(f.files, uid)
	return nil
}

func (f *fakePhotoWriter) ArchivePhoto(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.photos[uid]
	if !ok {
		return database.ErrNotFound
	}
	now := time.Now()
	p.ArchivedAt = &now
	return nil
}

func (f *fakePhotoWriter) RestorePhoto(_ context.Context, _ string) error {
	panic("not implemented")
}

func (f *fakePhotoWriter) AddPhotoFile(_ context.Context, file *database.PhotoFile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[file.PhotoUID] = append(f.files[file.PhotoUID], *file)
	return nil
}

func (f *fakePhotoWriter) DeletePhotoFile(_ context.Context, _, _ string) error {
	panic("not implemented")
}

// addPhoto inserts a photo with the given UID/hash. archivedAt is the
// archived_at timestamp; pass the zero value to leave the photo live.
func (f *fakePhotoWriter) addPhoto(uid, hash, relPath string, archivedAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &database.Photo{
		UID:      uid,
		FileHash: hash,
		FilePath: relPath,
		FileName: filepath.Base(relPath),
	}
	if !archivedAt.IsZero() {
		t := archivedAt
		p.ArchivedAt = &t
	}
	f.photos[uid] = p
}

// hasPhoto reports whether the given UID is still in the in-memory store.
func (f *fakePhotoWriter) hasPhoto(uid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.photos[uid]
	return ok
}

// fakeEmbeddingWriter is the smallest in-memory database.EmbeddingWriter
// that the trash helpers exercise. Only Has + DeleteEmbedding matter.
type fakeEmbeddingWriter struct {
	mu      sync.Mutex
	uids    map[string]bool
	deleted []string
}

func newFakeEmbeddingWriter() *fakeEmbeddingWriter {
	return &fakeEmbeddingWriter{uids: make(map[string]bool)}
}

func (f *fakeEmbeddingWriter) addEmbedding(uid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uids[uid] = true
}

func (f *fakeEmbeddingWriter) hasEmbedding(uid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uids[uid]
}

func (f *fakeEmbeddingWriter) Get(_ context.Context, uid string) (*database.StoredEmbedding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.uids[uid] {
		//nolint:nilnil // mirrors database.EmbeddingReader.Get contract:
		// (nil, nil) on absent rows is the conventional "no such row" signal.
		return nil, nil
	}
	return &database.StoredEmbedding{PhotoUID: uid}, nil
}

func (f *fakeEmbeddingWriter) Has(_ context.Context, uid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uids[uid], nil
}

func (f *fakeEmbeddingWriter) Count(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.uids), nil
}

func (f *fakeEmbeddingWriter) CountByUIDs(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (f *fakeEmbeddingWriter) FindSimilar(
	_ context.Context, _ []float32, _ int,
) ([]database.StoredEmbedding, error) {
	return nil, nil
}

func (f *fakeEmbeddingWriter) FindSimilarWithDistance(
	_ context.Context, _ []float32, _ int, _ float64,
) ([]database.StoredEmbedding, []float64, error) {
	return nil, nil, nil
}

func (f *fakeEmbeddingWriter) GetUniquePhotoUIDs(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeEmbeddingWriter) GetCentroid(_ context.Context, _ []string) ([]float32, error) {
	return nil, nil
}

func (f *fakeEmbeddingWriter) DeleteEmbedding(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.uids, uid)
	f.deleted = append(f.deleted, uid)
	return nil
}

// fakeFaceWriter is the smallest in-memory database.FaceWriter that the
// trash helpers exercise. DeleteFacesByPhoto records the UID; the rest of
// the interface stubs out.
type fakeFaceWriter struct {
	mu      sync.Mutex
	uids    map[string][]int64
	deleted []string
}

func newFakeFaceWriter() *fakeFaceWriter {
	return &fakeFaceWriter{uids: make(map[string][]int64)}
}

func (f *fakeFaceWriter) addFaces(uid string, ids ...int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uids[uid] = ids
}

func (f *fakeFaceWriter) hasFaces(uid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.uids[uid]
	return ok
}

func (f *fakeFaceWriter) GetFaces(_ context.Context, _ string) ([]database.StoredFace, error) {
	return []database.StoredFace{}, nil
}

func (f *fakeFaceWriter) GetFacesBySubjectName(
	_ context.Context, _ string,
) ([]database.StoredFace, error) {
	return nil, nil
}

func (f *fakeFaceWriter) HasFaces(_ context.Context, _ string) (bool, error) { return false, nil }

func (f *fakeFaceWriter) IsFacesProcessed(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeFaceWriter) Count(_ context.Context) (int, error)                   { return 0, nil }
func (f *fakeFaceWriter) CountByUIDs(_ context.Context, _ []string) (int, error) { return 0, nil }
func (f *fakeFaceWriter) CountPhotos(_ context.Context) (int, error)             { return 0, nil }
func (f *fakeFaceWriter) CountPhotosByUIDs(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (f *fakeFaceWriter) FindSimilar(
	_ context.Context, _ []float32, _ int,
) ([]database.StoredFace, error) {
	return nil, nil
}

func (f *fakeFaceWriter) FindSimilarWithDistance(
	_ context.Context, _ []float32, _ int, _ float64,
) ([]database.StoredFace, []float64, error) {
	return nil, nil, nil
}

func (f *fakeFaceWriter) GetUniquePhotoUIDs(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeFaceWriter) GetFacesWithMarkerUID(_ context.Context) ([]database.StoredFace, error) {
	return nil, nil
}

func (f *fakeFaceWriter) GetPhotoUIDsWithSubjectName(
	_ context.Context, _ []string, _ string,
) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (f *fakeFaceWriter) SaveFaces(
	_ context.Context, _ string, _ []database.StoredFace,
) error {
	return nil
}

func (f *fakeFaceWriter) MarkFacesProcessed(_ context.Context, _ string, _ int) error {
	return nil
}

func (f *fakeFaceWriter) UpdateFaceMarker(
	_ context.Context, _ string, _ int, _, _, _ string,
) error {
	return nil
}

func (f *fakeFaceWriter) UpdateFacePhotoInfo(
	_ context.Context, _ string, _, _, _ int, _ string,
) error {
	return nil
}

func (f *fakeFaceWriter) DeleteFacesByPhoto(
	_ context.Context, uid string,
) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := f.uids[uid]
	delete(f.uids, uid)
	f.deleted = append(f.deleted, uid)
	return ids, nil
}

// newTestStore wires the fakes plus a fresh storage.Storage rooted under
// t.TempDir(). The originals + thumb files for the test fixtures must
// also exist on disk for the purge step to assert that they were removed.
func newTestStore(t *testing.T) (*Store, *fakePhotoWriter, *fakeEmbeddingWriter, *fakeFaceWriter, *storage.Storage) {
	t.Helper()
	root := t.TempDir()
	s, err := storage.New(filepath.Join(root, "originals"), filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	photos := newFakePhotoWriter()
	embs := newFakeEmbeddingWriter()
	faces := newFakeFaceWriter()
	return &Store{
		Photos:     photos,
		Embeddings: embs,
		Faces:      faces,
		Files:      s,
	}, photos, embs, faces, s
}

// writeFixtureOriginal writes a small file under <originalsRoot>/<rel>.
// Returns the absolute path so the test can later assert that DeleteOriginal
// removed it.
func writeFixtureOriginal(t *testing.T, s *storage.Storage, rel string) string {
	t.Helper()
	abs, err := s.AbsOriginal(rel)
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}
	if writeErr := os.WriteFile(abs, []byte("dummy"), 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}
	return abs
}

// writeFixtureThumb writes a thumb at every entry in storage.ValidThumbSizes
// for the given file hash. Returns the absolute paths so the test can
// assert that every size was removed by the purge.
func writeFixtureThumbs(t *testing.T, s *storage.Storage, hash string) []string {
	t.Helper()
	paths := make([]string, 0, len(storage.ValidThumbSizes))
	for size := range storage.ValidThumbSizes {
		rel, err := storage.ThumbRelPath(hash, size)
		if err != nil {
			t.Fatalf("ThumbRelPath(%s,%s): %v", hash, size, err)
		}
		abs, err := s.AbsThumb(rel)
		if err != nil {
			t.Fatalf("AbsThumb: %v", err)
		}
		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			t.Fatalf("MkdirAll: %v", mkErr)
		}
		if writeErr := os.WriteFile(abs, []byte("thumb"), 0o644); writeErr != nil {
			t.Fatalf("WriteFile: %v", writeErr)
		}
		paths = append(paths, abs)
	}
	return paths
}

func TestPurgePhoto_NotArchived(t *testing.T) {
	t.Parallel()
	store, photos, embs, faces, _ := newTestStore(t)
	photos.addPhoto("p1", "abc123def456", "2024/06/p1.jpg", time.Time{})
	embs.addEmbedding("p1")
	faces.addFaces("p1", 1, 2)

	err := PurgePhoto(context.Background(), "p1", store)
	if !errors.Is(err, ErrNotArchived) {
		t.Fatalf("PurgePhoto on live photo: got %v, want ErrNotArchived", err)
	}
	if !photos.hasPhoto("p1") {
		t.Error("photo row should still exist when purge is rejected")
	}
	if !embs.hasEmbedding("p1") {
		t.Error("embedding row should still exist when purge is rejected")
	}
	if !faces.hasFaces("p1") {
		t.Error("face rows should still exist when purge is rejected")
	}
}

func TestPurgePhoto_NotFound(t *testing.T) {
	t.Parallel()
	store, _, _, _, _ := newTestStore(t)

	err := PurgePhoto(context.Background(), "missing", store)
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("PurgePhoto on missing uid: got %v, want ErrNotFound", err)
	}
}

func TestPurgePhoto_Success(t *testing.T) {
	t.Parallel()
	store, photos, embs, faces, fs := newTestStore(t)

	rel := "2024/06/p1.jpg"
	hash := "abc123def456" // first six chars yield directory shards "ab/c1/23".
	archivedAt := time.Now().Add(-31 * 24 * time.Hour)
	photos.addPhoto("p1", hash, rel, archivedAt)
	photos.files["p1"] = []database.PhotoFile{
		{ID: 1, PhotoUID: "p1", FilePath: rel, IsPrimary: true},
	}

	originalPath := writeFixtureOriginal(t, fs, rel)
	thumbPaths := writeFixtureThumbs(t, fs, hash)

	embs.addEmbedding("p1")
	faces.addFaces("p1", 7, 8)

	if err := PurgePhoto(context.Background(), "p1", store); err != nil {
		t.Fatalf("PurgePhoto: %v", err)
	}

	if photos.hasPhoto("p1") {
		t.Error("photo row should be deleted after purge")
	}
	if embs.hasEmbedding("p1") {
		t.Error("embedding row should be deleted after purge")
	}
	if faces.hasFaces("p1") {
		t.Error("face rows should be deleted after purge")
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Errorf("original file should be deleted; stat err = %v", err)
	}
	for _, p := range thumbPaths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("thumb %q should be deleted; stat err = %v", p, err)
		}
	}
}

func TestPurgePhoto_MissingFilesOnDiskNotFatal(t *testing.T) {
	t.Parallel()
	store, photos, embs, faces, _ := newTestStore(t)
	archivedAt := time.Now().Add(-31 * 24 * time.Hour)
	photos.addPhoto("p1", "abc123def456", "2024/06/p1.jpg", archivedAt)
	embs.addEmbedding("p1")
	faces.addFaces("p1", 7, 8)
	// Intentionally do not write any files on disk — the purge must still
	// drop the DB rows so a partially-cleaned trash converges on every
	// subsequent run.

	if err := PurgePhoto(context.Background(), "p1", store); err != nil {
		t.Fatalf("PurgePhoto with missing files: %v", err)
	}
	if photos.hasPhoto("p1") {
		t.Error("photo row should be deleted even when files are missing")
	}
}

func TestAutoPurge_RemovesOlderThanCutoff(t *testing.T) {
	t.Parallel()
	store, photos, embs, faces, fs := newTestStore(t)

	old := time.Now().Add(-31 * 24 * time.Hour)
	recent := time.Now().Add(-5 * 24 * time.Hour)

	photos.addPhoto("old", "ababab111111", "2024/05/old.jpg", old)
	photos.addPhoto("recent", "cdcdcd222222", "2024/06/recent.jpg", recent)
	photos.addPhoto("live", "efefef333333", "2024/06/live.jpg", time.Time{})

	embs.addEmbedding("old")
	embs.addEmbedding("recent")
	faces.addFaces("old", 1)
	faces.addFaces("recent", 2)
	writeFixtureOriginal(t, fs, "2024/05/old.jpg")
	writeFixtureOriginal(t, fs, "2024/06/recent.jpg")

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	purged, errs := AutoPurge(context.Background(), cutoff, store)
	if purged != 1 {
		t.Errorf("AutoPurge: purged=%d, want 1", purged)
	}
	if len(errs) != 0 {
		t.Errorf("AutoPurge: errs=%v", errs)
	}
	if photos.hasPhoto("old") {
		t.Error("old photo should be deleted")
	}
	if !photos.hasPhoto("recent") {
		t.Error("recent photo should be kept")
	}
	if !photos.hasPhoto("live") {
		t.Error("live photo should be kept")
	}
}

func TestAutoPurge_NoArchivedPhotos(t *testing.T) {
	t.Parallel()
	store, photos, _, _, _ := newTestStore(t)
	photos.addPhoto("live", "ffffff111111", "2024/06/live.jpg", time.Time{})

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	purged, errs := AutoPurge(context.Background(), cutoff, store)
	if purged != 0 {
		t.Errorf("purged=%d, want 0", purged)
	}
	if len(errs) != 0 {
		t.Errorf("errs=%v", errs)
	}
}

func TestRunDaemon_StopsOnCancel(t *testing.T) {
	t.Parallel()
	store, _, _, _, _ := newTestStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunDaemon(ctx, 50*time.Millisecond, time.Hour, store)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunDaemon did not return after context cancel")
	}
}
