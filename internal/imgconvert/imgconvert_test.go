package imgconvert

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireBinary skips the current test when the named external binary is
// not on PATH so CI/dev machines without it still pass.
func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed; skipping", name)
	}
}

// requireFixture skips the current test when the fixture path does not
// exist. The package ships without HEIC/RAW samples for licensing
// reasons; the spec explicitly allows the conversion tests to skip when
// fixtures are absent.
func requireFixture(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("test fixture %s missing; skipping", path)
	}
}

func TestIsSupportedFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ext  string
		want bool
	}{
		{".jpg", true},
		{".JPEG", true},
		{"png", true},
		{"webp", true},
		{".heic", true},
		{".HEIF", true},
		{".cr2", true},
		{".cr3", true},
		{".nef", true},
		{".arw", true},
		{".dng", true},
		{".raf", true},
		{".orf", true},
		{".rw2", true},
		{".pef", true},
		{".srw", true},
		{".tiff", false},
		{".gif", false},
		{".bmp", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsSupportedFormat(tc.ext); got != tc.want {
			t.Errorf("IsSupportedFormat(%q) = %v, want %v", tc.ext, got, tc.want)
		}
	}
}

func TestDetectFormat_ByMagic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct {
		name  string
		bytes []byte
		ext   string
		want  string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}, ".jpg", FormatJPEG},
		{"png", []byte("\x89PNG\r\n\x1a\nrest-of-png-payload"), ".png", FormatPNG},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 4)...), ".webp", FormatWebP},
		{
			"heic-brand-heic",
			[]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0},
			".heic", FormatHEIC,
		},
		{
			"heic-brand-heix",
			[]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'x', 0, 0, 0, 0},
			".heic", FormatHEIC,
		},
		{
			"heic-brand-mif1",
			[]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1', 0, 0, 0, 0},
			".heif", FormatHEIC,
		},
		{
			"raw-cr2-by-extension",
			// CR2 starts with TIFF magic; magic alone won't identify it
			// (TIFF != any of our formats), so DetectFormat falls back to
			// the .cr2 extension.
			[]byte{0x49, 0x49, 0x2A, 0x00, 0x10, 0x00, 0x00, 0x00, 'C', 'R', 0x02, 0x00},
			".cr2", FormatRAW,
		},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, tc.name+tc.ext)
		if err := os.WriteFile(path, tc.bytes, 0o600); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		if got := DetectFormat(path); got != tc.want {
			t.Errorf("DetectFormat(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDetectFormat_MagicWinsOverExtension(t *testing.T) {
	t.Parallel()
	// A JPEG renamed to .png — magic must win.
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(path, []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DetectFormat(path); got != FormatJPEG {
		t.Errorf("DetectFormat = %q, want jpeg (magic must beat extension)", got)
	}
}

func TestDetectFormat_Unknown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rando.xyz")
	if err := os.WriteFile(path, []byte("garbage data here, no magic match"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DetectFormat(path); got != FormatUnknown {
		t.Errorf("DetectFormat = %q, want unknown", got)
	}
}

func TestDetectFormat_NonexistentFile(t *testing.T) {
	t.Parallel()
	// A path that doesn't exist returns unknown via the extension lookup
	// (magicFormat silently treats the open failure as unknown).
	if got := DetectFormat("/nonexistent/path.jpg"); got != FormatJPEG {
		t.Errorf("DetectFormat on missing .jpg = %q, want jpeg (extension fallback)", got)
	}
}

func TestEnsureDecodable_PassThroughJPEG(t *testing.T) {
	t.Parallel()
	src := filepath.Join("..", "exif", "testdata", "basic.jpg")
	requireFixture(t, src)
	path, cleanup, err := EnsureDecodable(context.Background(), src)
	if err != nil {
		t.Fatalf("EnsureDecodable: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup must not be nil for pass-through")
	}
	if path != src {
		t.Errorf("path = %q, want %q (pass-through)", path, src)
	}
	// Pass-through cleanup is a no-op; calling it multiple times must be safe.
	cleanup()
	cleanup()
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source file removed by pass-through cleanup: %v", err)
	}
}

func TestEnsureDecodable_EmptyPath(t *testing.T) {
	t.Parallel()
	_, cleanup, err := EnsureDecodable(context.Background(), "")
	if err == nil {
		t.Fatal("expected error on empty path")
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}

func TestEnsureDecodable_NonexistentFile(t *testing.T) {
	t.Parallel()
	_, cleanup, err := EnsureDecodable(context.Background(), "/nonexistent/imgconvert-test.jpg")
	if err == nil {
		t.Fatal("expected error on nonexistent file")
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}

func TestEnsureDecodable_UnsupportedFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "weird.xyz")
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := EnsureDecodable(context.Background(), path)
	if err == nil {
		t.Fatal("expected error on unsupported format")
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}

func TestEnsureDecodable_HEIC(t *testing.T) {
	requireBinary(t, "heif-convert")
	requireFixture(t, "testdata/sample.heic")
	tmpPath, cleanup, err := EnsureDecodable(context.Background(), "testdata/sample.heic")
	if err != nil {
		t.Fatalf("EnsureDecodable HEIC: %v", err)
	}
	defer cleanup()
	if filepath.Ext(tmpPath) != ".jpg" {
		t.Errorf("output ext = %q, want .jpg", filepath.Ext(tmpPath))
	}
	info, statErr := os.Stat(tmpPath)
	if statErr != nil {
		t.Fatalf("stat output: %v", statErr)
	}
	if info.Size() == 0 {
		t.Errorf("converted JPEG is empty")
	}
}

func TestEnsureDecodable_RAW(t *testing.T) {
	requireBinary(t, "dcraw")
	requireFixture(t, "testdata/sample.cr2")
	tmpPath, cleanup, err := EnsureDecodable(context.Background(), "testdata/sample.cr2")
	if err != nil {
		t.Fatalf("EnsureDecodable RAW: %v", err)
	}
	defer cleanup()
	if filepath.Ext(tmpPath) != ".jpg" {
		t.Errorf("output ext = %q, want .jpg", filepath.Ext(tmpPath))
	}
	info, statErr := os.Stat(tmpPath)
	if statErr != nil {
		t.Fatalf("stat output: %v", statErr)
	}
	if info.Size() == 0 {
		t.Errorf("converted JPEG is empty")
	}
}

func TestConvertHEIC_BinaryMissing(t *testing.T) {
	if _, err := exec.LookPath("heif-convert"); err == nil {
		t.Skip("heif-convert is installed; sentinel-error test only runs without it")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f.heic")
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := convertHEIC(context.Background(), path)
	if err == nil {
		t.Fatal("expected error when heif-convert is missing")
	}
	if !errors.Is(err, ErrConverterMissing) {
		t.Errorf("err = %v, want wrap of ErrConverterMissing", err)
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}

func TestConvertRAW_BinaryMissing(t *testing.T) {
	if _, err := exec.LookPath("dcraw"); err == nil {
		t.Skip("dcraw is installed; sentinel-error test only runs without it")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f.cr2")
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := convertRAW(context.Background(), path)
	if err == nil {
		t.Fatal("expected error when dcraw is missing")
	}
	if !errors.Is(err, ErrConverterMissing) {
		t.Errorf("err = %v, want wrap of ErrConverterMissing", err)
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}

func TestEnsureDecodable_HEICWithoutBinary(t *testing.T) {
	if _, err := exec.LookPath("heif-convert"); err == nil {
		t.Skip("heif-convert is installed; missing-binary path only runs without it")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f.heic")
	// Real HEIC magic bytes so DetectFormat dispatches to convertHEIC.
	hdr := []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0}
	if err := os.WriteFile(path, hdr, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := EnsureDecodable(context.Background(), path)
	if err == nil {
		t.Fatal("expected error when heif-convert missing")
	}
	if !errors.Is(err, ErrConverterMissing) {
		t.Errorf("err = %v, want wrap of ErrConverterMissing", err)
	}
}

func TestDecodePPM_8Bit(t *testing.T) {
	t.Parallel()
	header := "P6\n3 2\n255\n"
	// 3x2 image: row 0 is pure R, G, B; row 1 is three shades of grey.
	pixels := []byte{
		0xFF, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00, 0xFF,
		0x80, 0x80, 0x80, 0x40, 0x40, 0x40, 0x20, 0x20, 0x20,
	}
	body := append([]byte(header), pixels...)
	img, err := decodePPM(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decodePPM: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 3 || bounds.Dy() != 2 {
		t.Fatalf("dims = %dx%d, want 3x2", bounds.Dx(), bounds.Dy())
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 0xFF || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("pixel (0,0) = %d,%d,%d, want pure red", r>>8, g>>8, b>>8)
	}
	r, g, b, _ = img.At(1, 1).RGBA()
	if r>>8 != 0x40 || g>>8 != 0x40 || b>>8 != 0x40 {
		t.Errorf("pixel (1,1) = %d,%d,%d, want 0x40 grey", r>>8, g>>8, b>>8)
	}
}

func TestDecodePPM_16Bit(t *testing.T) {
	t.Parallel()
	header := "P6\n1 1\n65535\n"
	// One pixel: R=0xAABB, G=0xCCDD, B=0xEEFF, big-endian.
	body := append([]byte(header), 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF)
	img, err := decodePPM(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decodePPM 16bit: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 0xAA || g>>8 != 0xCC || b>>8 != 0xEE {
		t.Errorf("downsample = %d,%d,%d, want 0xAA,0xCC,0xEE (high bytes)", r>>8, g>>8, b>>8)
	}
}

func TestDecodePPM_HeaderComments(t *testing.T) {
	t.Parallel()
	body := []byte("P6\n# this is a comment\n2 1\n# another comment\n255\n" +
		"\xFF\x00\x00\x00\xFF\x00")
	img, err := decodePPM(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decodePPM with comments: %v", err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Errorf("dims = %dx%d, want 2x1", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestDecodePPM_BadMagic(t *testing.T) {
	t.Parallel()
	// P3 is the ASCII PPM variant, which dcraw never emits and decodePPM
	// must reject.
	_, err := decodePPM(bytes.NewReader([]byte("P3\n1 1\n255\n255 0 0")))
	if err == nil {
		t.Fatal("expected error on P3 (ASCII PPM)")
	}
}

func TestDecodePPM_TruncatedPixels(t *testing.T) {
	t.Parallel()
	// Header advertises 2 pixels (6 bytes) but body only has 3.
	body := []byte("P6\n2 1\n255\n\xFF\x00\x00")
	_, err := decodePPM(bytes.NewReader(body))
	if err == nil {
		t.Fatal("expected error on truncated pixel data")
	}
}

func TestOnceRemove_IsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ephemeral")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup := onceRemove(path)
	cleanup()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("expected file to be removed after first cleanup call")
	}
	// Second call must not panic and must remain safe even though the
	// underlying file no longer exists.
	cleanup()
}
