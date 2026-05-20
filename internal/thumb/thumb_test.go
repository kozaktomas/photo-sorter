package thumb

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// testFileHash is a 64-character lowercase hex string. ThumbRelPath shards
// the first six characters into the cache tree.
const testFileHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// newTestStorage allocates an isolated Storage rooted under t.TempDir().
func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	root := t.TempDir()
	s, err := storage.New(filepath.Join(root, "originals"), filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s
}

// writeTestJPEG renders a deterministic gradient at width × height and
// JPEG-encodes it to a fresh file under dir. The returned path is
// suitable for use as Source.Path.
func writeTestJPEG(t *testing.T, dir string, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: 128,
				A: 255,
			})
		}
	}
	path := filepath.Join(dir, "source.jpg")
	f, err := os.Create(path) // #nosec G304 -- dir is t.TempDir()
	if err != nil {
		t.Fatalf("create %q: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}
	return path
}

// decodeJPEGBounds opens and decodes a JPEG file, returning its width and
// height. The test fails if anything goes wrong.
func decodeJPEGBounds(t *testing.T, path string) (width, height int) {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- path comes from storage.AbsThumb
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %q: %v", path, err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

// TestGenerateAll_producesAllSizesWithinBounds covers the spec test "a
// sample 4000×3000 JPEG → all 10 thumbnails produced, each within the
// size bound." Cannot run in parallel — the 4000×3000 RGBA + 10 resizes
// allocate enough memory that running concurrently with other tests on a
// small device can OOM.
func TestGenerateAll_producesAllSizesWithinBounds(t *testing.T) {
	store := newTestStorage(t)
	srcPath := writeTestJPEG(t, t.TempDir(), 4000, 3000)

	got, err := GenerateAll(Source{Path: srcPath}, store, testFileHash)
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if len(got) != len(sizes) {
		t.Fatalf("GenerateAll returned %d paths, want %d", len(got), len(sizes))
	}

	for name, spec := range sizes {
		rel, ok := got[name]
		if !ok {
			t.Errorf("size %q missing from result map", name)
			continue
		}
		if !store.ThumbExists(rel) {
			t.Errorf("size %q: thumb file %q does not exist", name, rel)
			continue
		}
		abs, err := store.AbsThumb(rel)
		if err != nil {
			t.Errorf("AbsThumb(%q): %v", rel, err)
			continue
		}
		w, h := decodeJPEGBounds(t, abs)
		switch spec.Mode {
		case modeFit:
			if w > spec.Max || h > spec.Max {
				t.Errorf("size %q: %dx%d exceeds max longest side %d", name, w, h, spec.Max)
			}
		case modeCropSquare:
			if w != spec.Max || h != spec.Max {
				t.Errorf("size %q: %dx%d, want %dx%d", name, w, h, spec.Max, spec.Max)
			}
		}
	}
}

// TestGenerateAll_orientation6SwapsDimensions covers the spec test for
// orientation 6 (90 degrees CW), where the resulting thumb must have its
// width and height swapped relative to the source.
func TestGenerateAll_orientation6SwapsDimensions(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	srcPath := writeTestJPEG(t, t.TempDir(), 800, 600)

	got, err := GenerateAll(Source{Path: srcPath, Orientation: 6}, store, testFileHash)
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	// Source is 800x600 (landscape). After applying orientation 6
	// (rotate 90 CW) the image is 600x800 (portrait). fit_720 scales
	// the new longest side (800) to 720, giving 540x720.
	rel := got["fit_720"]
	abs, err := store.AbsThumb(rel)
	if err != nil {
		t.Fatalf("AbsThumb: %v", err)
	}
	w, h := decodeJPEGBounds(t, abs)
	if h <= w {
		t.Errorf("orientation 6 should yield portrait thumb, got %dx%d", w, h)
	}
	if h != 720 {
		t.Errorf("expected height 720 after fit_720, got %d", h)
	}
	if w != 540 {
		t.Errorf("expected width 540 after fit_720, got %d", w)
	}
}

// TestGenerateAll_idempotent covers the spec test "re-running GenerateAll
// is a no-op (no rewrites)." It compares modification times before and
// after the second run — if a file was rewritten the rename(2) would
// bump its mtime past the captured value.
func TestGenerateAll_idempotent(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	srcPath := writeTestJPEG(t, t.TempDir(), 400, 300)

	first, err := GenerateAll(Source{Path: srcPath}, store, testFileHash)
	if err != nil {
		t.Fatalf("GenerateAll first: %v", err)
	}
	modTimes := make(map[string]time.Time, len(first))
	for name, rel := range first {
		abs, err := store.AbsThumb(rel)
		if err != nil {
			t.Fatalf("AbsThumb(%q): %v", rel, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatalf("stat %q: %v", abs, err)
		}
		modTimes[name] = info.ModTime()
	}

	// Sleep past the filesystem mtime granularity so a rewrite would
	// be detectable.
	time.Sleep(50 * time.Millisecond)

	second, err := GenerateAll(Source{Path: srcPath}, store, testFileHash)
	if err != nil {
		t.Fatalf("GenerateAll second: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("second run returned %d paths, want %d", len(second), len(first))
	}
	for name, rel := range second {
		if rel != first[name] {
			t.Errorf("size %q: path changed between runs (%q → %q)", name, first[name], rel)
		}
		abs, err := store.AbsThumb(rel)
		if err != nil {
			t.Fatalf("AbsThumb(%q): %v", rel, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatalf("stat %q: %v", abs, err)
		}
		if !info.ModTime().Equal(modTimes[name]) {
			t.Errorf("size %q: thumb was rewritten (mtime changed from %v to %v)",
				name, modTimes[name], info.ModTime())
		}
	}
}

// TestGenerate_unknownSizeReturnsError covers the spec test "unknown
// size name → error." The check happens before the source file is
// opened so no on-disk image is required.
func TestGenerate_unknownSizeReturnsError(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	_, err := Generate(Source{Path: "/nonexistent.jpg"}, "bogus_size", store, testFileHash)
	if err == nil {
		t.Fatal("Generate should reject unknown size name")
	}
}

// TestGenerate_singleSize confirms that calling Generate directly for one
// size also produces a thumb within bounds.
func TestGenerate_singleSize(t *testing.T) {
	t.Parallel()
	store := newTestStorage(t)
	srcPath := writeTestJPEG(t, t.TempDir(), 1200, 900)

	rel, err := Generate(Source{Path: srcPath}, "fit_1280", store, testFileHash)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !store.ThumbExists(rel) {
		t.Fatalf("thumb file %q does not exist", rel)
	}

	// Calling again should hit the "already exists" fast path and return
	// the same relative path without an error.
	rel2, err := Generate(Source{Path: srcPath}, "fit_1280", store, testFileHash)
	if err != nil {
		t.Fatalf("Generate second: %v", err)
	}
	if rel != rel2 {
		t.Errorf("second Generate returned %q, want %q", rel2, rel)
	}
}

// TestApplyOrientation_dimensionSwap exercises mapOrientation in
// isolation for every orientation value. Orientations 5-8 must swap
// width and height; orientations 2-4 keep them.
func TestApplyOrientation_dimensionSwap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		orientation        int
		wantSwapDimensions bool
	}{
		{1, false},
		{2, false},
		{3, false},
		{4, false},
		{5, true},
		{6, true},
		{7, true},
		{8, true},
		{0, false},
		{9, false},
	}
	src := image.NewRGBA(image.Rect(0, 0, 10, 4))
	for _, tc := range tests {
		got := applyOrientation(src, tc.orientation)
		b := got.Bounds()
		w, h := b.Dx(), b.Dy()
		if tc.wantSwapDimensions {
			if w != 4 || h != 10 {
				t.Errorf("orientation %d: got %dx%d, want 4x10", tc.orientation, w, h)
			}
		} else {
			if w != 10 || h != 4 {
				t.Errorf("orientation %d: got %dx%d, want 10x4", tc.orientation, w, h)
			}
		}
	}
}

// TestApplyOrientation_pixelMapping verifies the pixel-level transforms
// by placing a unique-colored pixel at the top-left of a small test
// image, applying each orientation, and checking the new location of
// that pixel.
func TestApplyOrientation_pixelMapping(t *testing.T) {
	t.Parallel()
	const w, h = 4, 3
	marker := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.Set(x, y, color.RGBA{R: 10, G: 10, B: 10, A: 255})
		}
	}
	src.Set(0, 0, marker)

	tests := []struct {
		orientation  int
		wantX, wantY int
	}{
		{2, w - 1, 0},     // mirror horizontal
		{3, w - 1, h - 1}, // rotate 180
		{4, 0, h - 1},     // mirror vertical
		{5, 0, 0},         // transpose (top-left stays top-left)
		{6, h - 1, 0},     // rotate 90 CW: top-left → top-right of new image (w=h, h=w)
		{7, h - 1, w - 1}, // transverse
		{8, 0, w - 1},     // rotate 90 CCW: top-left → bottom-left
	}
	for _, tc := range tests {
		got := applyOrientation(src, tc.orientation)
		r, g, b, a := got.At(tc.wantX, tc.wantY).RGBA()
		if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
			t.Errorf("orientation %d: marker not at (%d, %d); pixel RGBA = (%d, %d, %d, %d)",
				tc.orientation, tc.wantX, tc.wantY, r>>8, g>>8, b>>8, a>>8)
		}
	}
}
