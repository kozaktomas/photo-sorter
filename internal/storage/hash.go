package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// HashFile returns the lowercase hex-encoded SHA256 hash of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- callers are responsible for path safety
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	hash, _, err := HashReader(f)
	if err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hash, nil
}

// HashReader streams r through SHA256 and returns the lowercase hex-encoded
// hash together with the number of bytes read.
func HashReader(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", n, fmt.Errorf("copy to hasher: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
