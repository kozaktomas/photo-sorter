package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteOriginal writes the contents of r to the original at relPath
// atomically, hashing the bytes as they are written. It returns the number
// of bytes written and the lowercase hex-encoded SHA256 of the contents.
func (s *Storage) WriteOriginal(relPath string, r io.Reader) (int64, string, error) {
	dst, err := s.AbsOriginal(relPath)
	if err != nil {
		return 0, "", err
	}
	return atomicWrite(dst, r, true)
}

// WriteThumb writes the contents of r to the thumbnail at relPath atomically.
// Returns the number of bytes written.
func (s *Storage) WriteThumb(relPath string, r io.Reader) (int64, error) {
	dst, err := s.AbsThumb(relPath)
	if err != nil {
		return 0, err
	}
	n, _, err := atomicWrite(dst, r, false)
	return n, err
}

// atomicWrite writes the contents of r to dst via a temp file + rename.
// When wantHash is true, the SHA256 of the bytes is also computed and
// returned. On any error the temp file is removed.
func atomicWrite(dst string, r io.Reader, wantHash bool) (int64, string, error) {
	dir := filepath.Dir(dst)
	//nolint:gosec // G301: 0o755 matches PhotoPrism layout
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, "", fmt.Errorf("create parent dir %q: %w", dir, err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return 0, "", fmt.Errorf("generate temp suffix: %w", err)
	}
	tmp := dst + ".tmp." + suffix

	// O_EXCL guards against the (extremely unlikely) case of a colliding
	// suffix from a parallel writer.
	//nolint:gosec // G302: 0o644 matches PhotoPrism layout; G304: tmp path lives under our root
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, "", fmt.Errorf("create temp file %q: %w", tmp, err)
	}

	cleanupTmp := func() {
		// Best-effort cleanup; ignore errors.
		_ = os.Remove(tmp)
	}

	var dst64 int64
	var hash string
	if wantHash {
		h := sha256.New()
		mw := io.MultiWriter(f, h)
		n, copyErr := io.Copy(mw, r)
		dst64 = n
		if copyErr != nil {
			_ = f.Close()
			cleanupTmp()
			return n, "", fmt.Errorf("copy to temp file: %w", copyErr)
		}
		hash = hex.EncodeToString(h.Sum(nil))
	} else {
		n, copyErr := io.Copy(f, r)
		dst64 = n
		if copyErr != nil {
			_ = f.Close()
			cleanupTmp()
			return n, "", fmt.Errorf("copy to temp file: %w", copyErr)
		}
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanupTmp()
		return dst64, "", fmt.Errorf("sync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanupTmp()
		return dst64, "", fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		cleanupTmp()
		return dst64, "", fmt.Errorf("rename temp file: %w", err)
	}
	return dst64, hash, nil
}

// randomSuffix returns 16 hex characters drawn from crypto/rand.
func randomSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
