package latex

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

// downloadTestFixture spins up an httptest PhotoPrism mock that serves
// /photos/{uid} (returning a primary-file hash), /dl/{hash} (returning the
// original primary file), and /t/{hash}/{token}/{size} (returning a JPEG
// thumbnail used by the HEIC/RAW fallback path).
type downloadTestFixture struct {
	server   *httptest.Server
	pp       *photoprism.PhotoPrism
	primary  []byte // body served from /dl/{hash}
	thumb    []byte // body served from /t/{hash}/{token}/{size}
	dlHits   int
	thumbHit bool
}

func newDownloadTestFixture(t *testing.T, primary, thumb []byte) *downloadTestFixture {
	t.Helper()
	fx := &downloadTestFixture{primary: primary, thumb: thumb}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/photos/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Files": []any{
				map[string]any{"Primary": true, "Hash": "primaryhash"},
			},
		})
	})

	mux.HandleFunc("/api/v1/dl/primaryhash", func(w http.ResponseWriter, _ *http.Request) {
		fx.dlHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(fx.primary)
	})

	mux.HandleFunc("/api/v1/t/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "fit_7680") {
			http.NotFound(w, r)
			return
		}
		fx.thumbHit = true
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(fx.thumb)
	})

	fx.server = httptest.NewServer(mux)

	pp, err := photoprism.NewPhotoPrismFromToken(fx.server.URL, "session-tok", "dl-tok", "user-uid")
	if err != nil {
		fx.server.Close()
		t.Fatalf("NewPhotoPrismFromToken: %v", err)
	}
	fx.pp = pp
	return fx
}

func (fx *downloadTestFixture) Close() {
	fx.server.Close()
}

// makeSolidJPEG encodes a w x h solid-color JPEG and returns the raw bytes.
func makeSolidJPEG(t *testing.T, w, h int, q int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

// makeSolidPNG encodes a w x h solid-color PNG and returns the raw bytes.
func makeSolidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 50, G: 100, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadOriginalPhoto_FastPathPassThrough(t *testing.T) {
	primary := makeSolidJPEG(t, 100, 80, 75)
	fx := newDownloadTestFixture(t, primary, nil)
	defer fx.Close()

	tmpDir := t.TempDir()
	img, err := downloadOriginalPhoto(fx.pp, "test-uid", "primaryhash", tmpDir)
	if err != nil {
		t.Fatalf("downloadOriginalPhoto: %v", err)
	}

	got, err := os.ReadFile(img.path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}

	if !bytes.Equal(got, primary) {
		t.Errorf("fast path should be byte-identical: got %d bytes, want %d", len(got), len(primary))
	}
	if img.width != 100 || img.height != 80 {
		t.Errorf("dimensions: got %dx%d want 100x80", img.width, img.height)
	}
	if fx.thumbHit {
		t.Errorf("thumbnail endpoint should not be called on fast path")
	}

	// .orig temp must be cleaned up.
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".orig") {
			t.Errorf("temp file %q was not cleaned up", e.Name())
		}
	}
}

func TestDownloadOriginalPhoto_SlowPathPNGReencode(t *testing.T) {
	primary := makeSolidPNG(t, 120, 90)
	fx := newDownloadTestFixture(t, primary, nil)
	defer fx.Close()

	tmpDir := t.TempDir()
	img, err := downloadOriginalPhoto(fx.pp, "test-uid", "primaryhash", tmpDir)
	if err != nil {
		t.Fatalf("downloadOriginalPhoto: %v", err)
	}

	// Output must be a JPEG (re-encoded), not the original PNG bytes.
	got, err := os.ReadFile(img.path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if bytes.Equal(got, primary) {
		t.Errorf("slow path should re-encode, but result is byte-identical to PNG source")
	}

	if _, format, err := image.DecodeConfig(bytes.NewReader(got)); err != nil {
		t.Fatalf("result not decodable: %v", err)
	} else if format != "jpeg" {
		t.Errorf("result format: got %q want jpeg", format)
	}

	if img.width != 120 || img.height != 90 {
		t.Errorf("dimensions: got %dx%d want 120x90", img.width, img.height)
	}
	if fx.thumbHit {
		t.Errorf("thumbnail fallback should not be hit for decodable PNG")
	}
}

func TestDownloadOriginalPhoto_FallbackOnUndecodableSource(t *testing.T) {
	// Garbage bytes (HEIC/RAW emulation: pure-Go decoders all reject this).
	primary := []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic")
	thumb := makeSolidJPEG(t, 60, 40, 70)

	fx := newDownloadTestFixture(t, primary, thumb)
	defer fx.Close()

	tmpDir := t.TempDir()
	img, err := downloadOriginalPhoto(fx.pp, "test-uid", "primaryhash", tmpDir)
	if err != nil {
		t.Fatalf("downloadOriginalPhoto: %v", err)
	}

	if !fx.thumbHit {
		t.Errorf("expected fit_7680 thumbnail fallback to be requested")
	}

	got, err := os.ReadFile(img.path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if !bytes.Equal(got, thumb) {
		t.Errorf("fallback should write thumbnail bytes verbatim")
	}
	if img.width != 60 || img.height != 40 {
		t.Errorf("dimensions: got %dx%d want 60x40", img.width, img.height)
	}
}

func TestDownloadOriginalPhoto_StreamingDoesNotBufferFullBody(t *testing.T) {
	// Sanity-check that GetPhotoDownloadStream returns a body the caller can
	// stream and close. We assert exactly one /dl hit per call (no retry).
	primary := makeSolidJPEG(t, 50, 50, 80)
	fx := newDownloadTestFixture(t, primary, nil)
	defer fx.Close()

	tmpDir := t.TempDir()
	if _, err := downloadOriginalPhoto(fx.pp, "test-uid", "primaryhash", tmpDir); err != nil {
		t.Fatalf("downloadOriginalPhoto: %v", err)
	}
	if fx.dlHits != 1 {
		t.Errorf("dl endpoint hits: got %d want 1", fx.dlHits)
	}
}
