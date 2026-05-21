package cmd

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
)

// fakePhotoReader is an in-memory PhotoReader that lets the build-thumbs
// tests exercise the worker pool without spinning up PostgreSQL.
type fakePhotoReader struct {
	byUID map[string]*database.Photo
	order []string
}

func newFakePhotoReader(photos []*database.Photo) *fakePhotoReader {
	r := &fakePhotoReader{byUID: make(map[string]*database.Photo, len(photos))}
	for _, p := range photos {
		r.byUID[p.UID] = p
		r.order = append(r.order, p.UID)
	}
	return r
}

func (r *fakePhotoReader) GetPhoto(_ context.Context, uid string) (*database.Photo, error) {
	p, ok := r.byUID[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	clone := *p
	return &clone, nil
}

func (r *fakePhotoReader) GetPhotoByHash(_ context.Context, hash string) (*database.Photo, error) {
	for _, p := range r.byUID {
		if p.FileHash == hash {
			clone := *p
			return &clone, nil
		}
	}
	return nil, database.ErrNotFound
}

func (r *fakePhotoReader) ListPhotos(
	_ context.Context, filter database.PhotoFilter,
) ([]database.Photo, int, error) {
	out := make([]database.Photo, 0, len(r.order))
	for _, uid := range r.order {
		out = append(out, *r.byUID[uid])
	}
	total := len(out)
	if filter.Offset >= len(out) {
		return nil, total, nil
	}
	out = out[filter.Offset:]
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, total, nil
}

func (r *fakePhotoReader) ListPhotoFiles(_ context.Context, _ string) ([]database.PhotoFile, error) {
	return nil, nil
}

func (r *fakePhotoReader) ListArchivedBefore(_ context.Context, _ time.Time) ([]string, error) {
	return nil, nil
}

// writeBuildThumbFixture creates a fresh storage tree containing 3 photo
// originals with deterministic JPEG content and returns the storage
// handle, a slice of database.Photo rows (with file_path/file_hash/
// orientation populated to point at the on-disk files), and a thumb size
// list to use for assertions.
func writeBuildThumbFixture(t *testing.T) (*storage.Storage, []*database.Photo, []string) {
	t.Helper()
	root := t.TempDir()
	originalsRoot := filepath.Join(root, "originals")
	cacheRoot := filepath.Join(root, "cache")
	store, err := storage.New(originalsRoot, cacheRoot)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	// Three distinct file hashes so each photo has its own thumb shard.
	hashes := []string{
		"aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
		"112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00",
		"99887766554433221100ffeeddccbbaa99887766554433221100ffeeddccbbaa",
	}
	names := []string{"alpha.jpg", "beta.jpg", "gamma.jpg"}
	photos := make([]*database.Photo, 0, len(hashes))

	for i, h := range hashes {
		// Make each image a different size so the resizes have something
		// to work with; small enough to keep the test fast on the Pi.
		w, hgt := 400+i*100, 300+i*60
		relDir := filepath.Join("2024", "06")
		dir := filepath.Join(originalsRoot, relDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		path := filepath.Join(dir, names[i])
		if err := writeJPEGFixture(path, w, hgt); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
		photos = append(photos, &database.Photo{
			UID:        "p" + h[:6] + "test" + names[i][:3],
			FileHash:   h,
			FilePath:   filepath.ToSlash(filepath.Join(relDir, names[i])),
			FileName:   names[i],
			FileMime:   "image/jpeg",
			FileWidth:  w,
			FileHeight: hgt,
		})
	}

	// Trim the size set down so the test is fast — the thumb package's
	// own unit tests cover the full registry.
	sizes := []string{"fit_720", "tile_100"}
	return store, photos, sizes
}

// writeJPEGFixture renders a deterministic gradient JPEG at the given
// size to dst.
func writeJPEGFixture(dst string, width, height int) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return err
	}
	return os.WriteFile(dst, buf.Bytes(), 0o644)
}

// snapshotThumbModTimes returns a map keyed by relative thumb path of the
// on-disk mtime for every (photo, size) pair that currently exists. The
// "no rewrites on re-run" test compares two snapshots.
func snapshotThumbModTimes(
	t *testing.T, store *storage.Storage,
	photos []*database.Photo, sizes []string,
) map[string]time.Time {
	t.Helper()
	out := make(map[string]time.Time)
	for _, p := range photos {
		for _, name := range sizes {
			rel, err := storage.ThumbRelPath(p.FileHash, name)
			if err != nil {
				t.Fatalf("ThumbRelPath: %v", err)
			}
			if !store.ThumbExists(rel) {
				continue
			}
			abs, err := store.AbsThumb(rel)
			if err != nil {
				t.Fatalf("AbsThumb: %v", err)
			}
			info, err := os.Stat(abs)
			if err != nil {
				t.Fatalf("stat %s: %v", abs, err)
			}
			out[rel] = info.ModTime()
		}
	}
	return out
}

// assertThumbsExist confirms that for every photo × size pair listed,
// the expected thumb file is on disk.
func assertThumbsExist(
	t *testing.T, store *storage.Storage,
	photos []*database.Photo, sizes []string,
) {
	t.Helper()
	for _, p := range photos {
		for _, name := range sizes {
			rel, err := storage.ThumbRelPath(p.FileHash, name)
			if err != nil {
				t.Fatalf("ThumbRelPath(%s, %s): %v", p.FileHash, name, err)
			}
			if !store.ThumbExists(rel) {
				t.Errorf("expected thumb to exist: %s (%s)", rel, name)
			}
		}
	}
}

// TestBuildThumbsForPhotos_onlyMissing seeds three photos, runs the
// backfill once, and verifies that every requested thumb size exists on
// disk afterward. Re-running must be a no-op (no file rewrites).
func TestBuildThumbsForPhotos_onlyMissing(t *testing.T) {
	t.Parallel()
	store, photos, sizes := writeBuildThumbFixture(t)
	deps := &buildThumbsDeps{
		photoReader: newFakePhotoReader(photos),
		store:       store,
		sizes:       sizes,
		onlyMissing: true,
	}

	uids := make([]string, 0, len(photos))
	for _, p := range photos {
		uids = append(uids, p.UID)
	}
	sort.Strings(uids) // deterministic across map iteration

	generated, skipped, failed := buildThumbsForPhotos(
		context.Background(), deps, uids, 2, nil,
	)
	if failed != 0 {
		t.Fatalf("first run: failed = %d, want 0", failed)
	}
	if skipped != 0 {
		t.Errorf("first run: skipped = %d, want 0", skipped)
	}
	wantGenerated := int64(len(photos) * len(sizes))
	if generated != wantGenerated {
		t.Errorf("first run: generated = %d, want %d", generated, wantGenerated)
	}

	assertThumbsExist(t, store, photos, sizes)
	firstSnapshot := snapshotThumbModTimes(t, store, photos, sizes)

	// Pause past common filesystem mtime granularity so a rewrite would
	// actually move the mtime forward.
	time.Sleep(50 * time.Millisecond)

	generated2, skipped2, failed2 := buildThumbsForPhotos(
		context.Background(), deps, uids, 2, nil,
	)
	if failed2 != 0 {
		t.Fatalf("re-run: failed = %d, want 0", failed2)
	}
	if generated2 != 0 {
		t.Errorf("re-run: generated = %d, want 0 (all thumbs already cached)", generated2)
	}
	if skipped2 != int64(len(photos)) {
		t.Errorf("re-run: skipped = %d, want %d", skipped2, len(photos))
	}

	secondSnapshot := snapshotThumbModTimes(t, store, photos, sizes)
	for rel, first := range firstSnapshot {
		second, ok := secondSnapshot[rel]
		if !ok {
			t.Errorf("thumb %q disappeared on re-run", rel)
			continue
		}
		if !first.Equal(second) {
			t.Errorf("thumb %q was rewritten (mtime %v → %v)", rel, first, second)
		}
	}
}

// TestBuildThumbsForPhotos_singlePhotoRegenerate covers the spec test
// "single-photo regen works": running the backfill with onlyMissing=false
// against one UID forces every requested size for that photo to be
// rewritten, while leaving other photos untouched.
func TestBuildThumbsForPhotos_singlePhotoRegenerate(t *testing.T) {
	t.Parallel()
	store, photos, sizes := writeBuildThumbFixture(t)
	deps := &buildThumbsDeps{
		photoReader: newFakePhotoReader(photos),
		store:       store,
		sizes:       sizes,
		onlyMissing: true,
	}

	// Seed thumbs for every photo first.
	uids := make([]string, 0, len(photos))
	for _, p := range photos {
		uids = append(uids, p.UID)
	}
	if _, _, failed := buildThumbsForPhotos(context.Background(), deps, uids, 2, nil); failed != 0 {
		t.Fatalf("seed run: failed = %d, want 0", failed)
	}
	beforeSnapshot := snapshotThumbModTimes(t, store, photos, sizes)
	time.Sleep(50 * time.Millisecond)

	// Now force-regenerate only the first photo's thumbs.
	deps.onlyMissing = false
	target := photos[0].UID
	generated, skipped, failed := buildThumbsForPhotos(
		context.Background(), deps, []string{target}, 1, nil,
	)
	if failed != 0 {
		t.Fatalf("regen: failed = %d, want 0", failed)
	}
	if skipped != 0 {
		t.Errorf("regen: skipped = %d, want 0", skipped)
	}
	if generated != int64(len(sizes)) {
		t.Errorf("regen: generated = %d, want %d", generated, len(sizes))
	}
	afterSnapshot := snapshotThumbModTimes(t, store, photos, sizes)

	// Target photo's thumbs must be rewritten (mtime advanced).
	for _, name := range sizes {
		rel, err := storage.ThumbRelPath(photos[0].FileHash, name)
		if err != nil {
			t.Fatalf("ThumbRelPath: %v", err)
		}
		before := beforeSnapshot[rel]
		after, ok := afterSnapshot[rel]
		if !ok {
			t.Errorf("regen: target thumb %q missing afterward", rel)
			continue
		}
		if !after.After(before) {
			t.Errorf("regen: target thumb %q mtime did not advance (%v → %v)", rel, before, after)
		}
	}

	// Other photos' thumbs must be untouched.
	for _, p := range photos[1:] {
		for _, name := range sizes {
			rel, err := storage.ThumbRelPath(p.FileHash, name)
			if err != nil {
				t.Fatalf("ThumbRelPath: %v", err)
			}
			if !beforeSnapshot[rel].Equal(afterSnapshot[rel]) {
				t.Errorf("regen: untouched photo %q size %q was rewritten", p.UID, name)
			}
		}
	}
}

// TestCollectPhotoUIDsForThumbs_photoUID short-circuits to a single-UID
// slice when --photo-uid is set, and returns an error for an unknown UID.
func TestCollectPhotoUIDsForThumbs_photoUID(t *testing.T) {
	t.Parallel()
	_, photos, _ := writeBuildThumbFixture(t)
	reader := newFakePhotoReader(photos)

	uids, err := collectPhotoUIDsForThumbs(
		context.Background(), reader, photos[1].UID, 0, true,
	)
	if err != nil {
		t.Fatalf("collectPhotoUIDsForThumbs(known UID): %v", err)
	}
	if len(uids) != 1 || uids[0] != photos[1].UID {
		t.Errorf("expected [%s], got %v", photos[1].UID, uids)
	}

	if _, err := collectPhotoUIDsForThumbs(
		context.Background(), reader, "does-not-exist", 0, true,
	); err == nil {
		t.Error("expected error for unknown UID, got nil")
	}
}

// TestCollectPhotoUIDsForThumbs_limit confirms that the --limit flag caps
// the number of UIDs returned across pagination boundaries.
func TestCollectPhotoUIDsForThumbs_limit(t *testing.T) {
	t.Parallel()
	_, photos, _ := writeBuildThumbFixture(t)
	reader := newFakePhotoReader(photos)

	uids, err := collectPhotoUIDsForThumbs(
		context.Background(), reader, "", 2, true,
	)
	if err != nil {
		t.Fatalf("collectPhotoUIDsForThumbs: %v", err)
	}
	if len(uids) != 2 {
		t.Errorf("limit=2: returned %d UIDs, want 2", len(uids))
	}
}

// TestResolveSizes covers the default-empty case, explicit override, and
// the unknown-size error path.
func TestResolveSizes(t *testing.T) {
	t.Parallel()
	full := thumb.SizeNames()
	got, err := resolveSizes(nil)
	if err != nil {
		t.Fatalf("resolveSizes(nil): %v", err)
	}
	if len(got) != len(full) {
		t.Errorf("default: got %d sizes, want %d", len(got), len(full))
	}

	got, err = resolveSizes([]string{"fit_720", "tile_100"})
	if err != nil {
		t.Fatalf("resolveSizes(known): %v", err)
	}
	if len(got) != 2 || got[0] != "fit_720" || got[1] != "tile_100" {
		t.Errorf("explicit: got %v, want [fit_720 tile_100]", got)
	}

	if _, err := resolveSizes([]string{"fit_999"}); err == nil {
		t.Error("expected error for unknown size, got nil")
	}
}
