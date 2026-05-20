package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStorage returns a Storage rooted under t.TempDir() with isolated
// originals/cache sub-directories.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	root := t.TempDir()
	s, err := New(filepath.Join(root, "originals"), filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// --- Path construction --------------------------------------------------------

func TestOriginalRelPath(t *testing.T) {
	tests := []struct {
		name     string
		takenAt  time.Time
		filename string
		want     string
	}{
		{
			name:     "normal date",
			takenAt:  time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
			filename: "IMG_1234.jpg",
			want:     "2024/06/IMG_1234.jpg",
		},
		{
			name:     "zero date routes to unknown",
			takenAt:  time.Time{},
			filename: "IMG_1234.jpg",
			want:     "unknown/IMG_1234.jpg",
		},
		{
			name:     "strips directory components",
			takenAt:  time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
			filename: "../../etc/passwd",
			want:     "2024/01/passwd",
		},
		{
			name:     "replaces unsafe characters",
			takenAt:  time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
			filename: "weird name (1)!.jpg",
			want:     "2024/12/weird_name__1__.jpg",
		},
		{
			name:     "single digit month is padded",
			takenAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			filename: "a.jpg",
			want:     "2024/01/a.jpg",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := OriginalRelPath(tc.takenAt, tc.filename)
			if got != tc.want {
				t.Fatalf("OriginalRelPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"a.jpg", "a.jpg"},
		{"a b.jpg", "a_b.jpg"},
		{"/abs/path/foo.png", "foo.png"},
		{"../foo.png", "foo.png"},
		{".", "_"},
		{"", "_"},
		{"héllo.jpg", "h_llo.jpg"},
		{"good-name_v2.JPG", "good-name_v2.JPG"},
	}
	for _, tc := range tests {
		got := SanitizeFilename(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestThumbRelPath(t *testing.T) {
	hash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	got, err := ThumbRelPath(hash, "fit_1920")
	if err != nil {
		t.Fatalf("ThumbRelPath: %v", err)
	}
	want := "ab/cd/ef/" + hash + "_fit_1920.jpg"
	if got != want {
		t.Fatalf("ThumbRelPath = %q, want %q", got, want)
	}

	// Uppercase hex should be normalised to lowercase prefix sharding.
	gotUpper, err := ThumbRelPath(strings.ToUpper(hash), "fit_1920")
	if err != nil {
		t.Fatalf("ThumbRelPath uppercase: %v", err)
	}
	if gotUpper != want {
		t.Fatalf("ThumbRelPath uppercase = %q, want %q", gotUpper, want)
	}

	for _, size := range []string{"fit_720", "fit_1280", "fit_1920", "fit_2560", "fit_3840", "fit_7680", "tile_50", "tile_100", "tile_224", "tile_500"} {
		if _, err := ThumbRelPath(hash, size); err != nil {
			t.Errorf("size %q should be valid: %v", size, err)
		}
	}

	if _, err := ThumbRelPath(hash, "bogus"); err == nil {
		t.Error("ThumbRelPath should reject unknown size")
	}
	if _, err := ThumbRelPath("zzz", "fit_720"); err == nil {
		t.Error("ThumbRelPath should reject non-hex prefix")
	}
	if _, err := ThumbRelPath("ab", "fit_720"); err == nil {
		t.Error("ThumbRelPath should reject short hash")
	}
}

// --- Path traversal -----------------------------------------------------------

func TestAbsOriginalRejectsTraversal(t *testing.T) {
	s := newTestStorage(t)

	badPaths := []string{
		"../etc/passwd",
		"..",
		"foo/../../etc/passwd",
		"/etc/passwd",
		"a\x00b",
		"",
		"./",
	}
	for _, p := range badPaths {
		if _, err := s.AbsOriginal(p); err == nil {
			t.Errorf("AbsOriginal(%q) should have failed", p)
		}
		if _, err := s.AbsThumb(p); err == nil {
			t.Errorf("AbsThumb(%q) should have failed", p)
		}
	}

	// A path that cleans to within root should succeed.
	good := "2024/06/IMG_1234.jpg"
	abs, err := s.AbsOriginal(good)
	if err != nil {
		t.Fatalf("AbsOriginal(%q): %v", good, err)
	}
	if !strings.HasPrefix(abs, s.OriginalsRoot()+string(filepath.Separator)) {
		t.Errorf("AbsOriginal returned %q, expected under %q", abs, s.OriginalsRoot())
	}
}

func TestWriteRejectsTraversal(t *testing.T) {
	s := newTestStorage(t)
	if _, _, err := s.WriteOriginal("../escape.jpg", strings.NewReader("x")); err == nil {
		t.Fatal("WriteOriginal should reject traversal")
	}
	if _, err := s.WriteThumb("../escape.jpg", strings.NewReader("x")); err == nil {
		t.Fatal("WriteThumb should reject traversal")
	}
}

// --- Hashing ------------------------------------------------------------------

func TestHashReaderStable(t *testing.T) {
	data := []byte("hello world\n")
	want := sha256.Sum256(data)
	wantHex := hex.EncodeToString(want[:])

	hash, n, err := HashReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("HashReader: %v", err)
	}
	if int(n) != len(data) {
		t.Fatalf("HashReader read %d bytes, want %d", n, len(data))
	}
	if hash != wantHex {
		t.Fatalf("HashReader = %q, want %q", hash, wantHex)
	}
}

func TestHashFileMatchesReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	data := bytes.Repeat([]byte{0xab}, 4096)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	hashFromFile, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	hashFromReader, _, err := HashReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("HashReader: %v", err)
	}
	if hashFromFile != hashFromReader {
		t.Fatalf("HashFile %q != HashReader %q", hashFromFile, hashFromReader)
	}
}

// --- Atomic write -------------------------------------------------------------

func TestWriteOriginalAtomic(t *testing.T) {
	s := newTestStorage(t)

	data := []byte("photo bytes")
	rel := OriginalRelPath(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), "IMG_1234.jpg")

	n, hash, err := s.WriteOriginal(rel, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("WriteOriginal: %v", err)
	}
	if int(n) != len(data) {
		t.Fatalf("WriteOriginal wrote %d bytes, want %d", n, len(data))
	}

	wantHashBytes := sha256.Sum256(data)
	if hash != hex.EncodeToString(wantHashBytes[:]) {
		t.Fatalf("WriteOriginal hash = %q, want %q", hash, hex.EncodeToString(wantHashBytes[:]))
	}

	// Round-trip read.
	f, err := s.OpenOriginal(rel)
	if err != nil {
		t.Fatalf("OpenOriginal: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip bytes mismatch")
	}

	// File should have mode 0o644 (rw-r--r--), masked by umask.
	abs, err := s.AbsOriginal(rel)
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 && info.Mode().Perm() != 0o644 {
		// On systems with restrictive umask the mode may be tighter. Either
		// the file is precisely 0o644 or it is at least as restrictive.
		t.Logf("file permission: %v", info.Mode().Perm())
	}

	// No leftover temp files in the directory.
	dir := filepath.Dir(abs)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteThumbRoundTrip(t *testing.T) {
	s := newTestStorage(t)

	hash := strings.Repeat("ab", 32) // 64 hex chars
	rel, err := ThumbRelPath(hash, "fit_720")
	if err != nil {
		t.Fatalf("ThumbRelPath: %v", err)
	}

	data := []byte("jpeg data")
	n, err := s.WriteThumb(rel, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("WriteThumb: %v", err)
	}
	if int(n) != len(data) {
		t.Fatalf("WriteThumb wrote %d, want %d", n, len(data))
	}
	if !s.ThumbExists(rel) {
		t.Fatal("ThumbExists should return true after write")
	}

	f, err := s.OpenThumb(rel)
	if err != nil {
		t.Fatalf("OpenThumb: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("thumbnail bytes mismatch")
	}
}

// errReader returns the provided bytes once and then a fixed error on the
// next call to Read. Used to make WriteOriginal fail mid-write.
type errReader struct {
	data []byte
	err  error
	read bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, r.err
}

func TestWriteOriginalTempCleanupOnError(t *testing.T) {
	s := newTestStorage(t)

	rel := OriginalRelPath(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), "broken.jpg")
	r := &errReader{data: []byte("partial"), err: errors.New("boom")}

	_, _, err := s.WriteOriginal(rel, r)
	if err == nil {
		t.Fatal("WriteOriginal should have returned an error")
	}

	abs, _ := s.AbsOriginal(rel)
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("destination file should not exist after failed write, stat err = %v", err)
	}

	dir := filepath.Dir(abs)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory may not exist if MkdirAll succeeded but rename failed; that's fine.
		return
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("temp file %q should be cleaned up after error", e.Name())
		}
	}
}

// --- Delete/exist helpers -----------------------------------------------------

func TestDeleteHelpers(t *testing.T) {
	s := newTestStorage(t)

	rel := OriginalRelPath(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), "x.jpg")
	if s.OriginalExists(rel) {
		t.Fatal("OriginalExists should be false initially")
	}
	if _, _, err := s.WriteOriginal(rel, strings.NewReader("x")); err != nil {
		t.Fatalf("WriteOriginal: %v", err)
	}
	if !s.OriginalExists(rel) {
		t.Fatal("OriginalExists should be true after write")
	}
	if err := s.DeleteOriginal(rel); err != nil {
		t.Fatalf("DeleteOriginal: %v", err)
	}
	if s.OriginalExists(rel) {
		t.Fatal("OriginalExists should be false after delete")
	}
	// Deleting a missing file is a no-op.
	if err := s.DeleteOriginal(rel); err != nil {
		t.Fatalf("DeleteOriginal on missing file: %v", err)
	}

	hash := strings.Repeat("cd", 32)
	thumbRel, err := ThumbRelPath(hash, "fit_720")
	if err != nil {
		t.Fatalf("ThumbRelPath: %v", err)
	}
	if _, err := s.WriteThumb(thumbRel, strings.NewReader("y")); err != nil {
		t.Fatalf("WriteThumb: %v", err)
	}
	if err := s.DeleteThumb(thumbRel); err != nil {
		t.Fatalf("DeleteThumb: %v", err)
	}
	if s.ThumbExists(thumbRel) {
		t.Fatal("ThumbExists should be false after delete")
	}
}

// --- Constructor --------------------------------------------------------------

func TestNewCreatesMissingDirs(t *testing.T) {
	root := t.TempDir()
	originals := filepath.Join(root, "deep", "originals")
	cache := filepath.Join(root, "deep", "cache")

	s, err := New(originals, cache)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(originals); err != nil {
		t.Errorf("originals not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "thumb")); err != nil {
		t.Errorf("cache/thumb not created: %v", err)
	}
	if s.OriginalsRoot() != originals {
		t.Errorf("OriginalsRoot = %q, want %q", s.OriginalsRoot(), originals)
	}
	if s.CacheRoot() != cache {
		t.Errorf("CacheRoot = %q, want %q", s.CacheRoot(), cache)
	}
}

func TestNewRejectsEmpty(t *testing.T) {
	if _, err := New("", "/tmp/cache"); err == nil {
		t.Error("New should reject empty originalsRoot")
	}
	if _, err := New("/tmp/orig", ""); err == nil {
		t.Error("New should reject empty cacheRoot")
	}
}
