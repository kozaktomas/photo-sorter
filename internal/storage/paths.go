package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ValidThumbSizes lists the thumbnail size names accepted by ThumbRelPath.
// These mirror the PhotoPrism cache layout.
var ValidThumbSizes = map[string]struct{}{
	"fit_720":  {},
	"fit_1280": {},
	"fit_1920": {},
	"fit_2560": {},
	"fit_3840": {},
	"fit_7680": {},
	"tile_50":  {},
	"tile_100": {},
	"tile_224": {},
	"tile_500": {},
}

// unsafeFilenameChars matches any character that is not a word character
// (letters, digits, underscore), a dot, or a hyphen.
var unsafeFilenameChars = regexp.MustCompile(`[^\w.-]`)

// hexShard6 matches exactly six lowercase hex characters — the prefix used
// to shard the thumbnail cache into three two-character directories.
var hexShard6 = regexp.MustCompile(`^[0-9a-f]{6}`)

// SanitizeFilename strips any path components from name and replaces any
// character outside of [\w.-] with '_'. Returns "_" when the result would be
// empty so callers always get a non-empty, safe segment.
func SanitizeFilename(name string) string {
	// Strip path components — only the base name matters.
	name = filepath.Base(name)
	// filepath.Base returns "." or "/" for degenerate inputs.
	if name == "." || name == "/" || name == string(filepath.Separator) {
		return "_"
	}
	cleaned := unsafeFilenameChars.ReplaceAllString(name, "_")
	if cleaned == "" {
		return "_"
	}
	return cleaned
}

// OriginalRelPath returns the relative path of a photo original under the
// originals root, in PhotoPrism style: YYYY/MM/<filename> when takenAt is
// known, unknown/<filename> otherwise. The filename is sanitized.
func OriginalRelPath(takenAt time.Time, filename string) string {
	safe := SanitizeFilename(filename)
	if takenAt.IsZero() {
		return filepath.ToSlash(filepath.Join("unknown", safe))
	}
	return filepath.ToSlash(filepath.Join(
		fmt.Sprintf("%04d", takenAt.Year()),
		fmt.Sprintf("%02d", int(takenAt.Month())),
		safe,
	))
}

// ThumbRelPath returns the relative path of a cached thumbnail under
// <cacheRoot>/thumb in PhotoPrism style: <aa>/<bb>/<cc>/<hash>_<size>.jpg
// where aa/bb/cc are the first six hex characters of fileHash split into
// three two-character segments. Returns an error if sizeName is not in
// ValidThumbSizes or fileHash does not start with at least 6 lowercase hex
// characters.
func ThumbRelPath(fileHash, sizeName string) (string, error) {
	if _, ok := ValidThumbSizes[sizeName]; !ok {
		return "", fmt.Errorf("invalid thumbnail size %q", sizeName)
	}
	fileHash = strings.ToLower(fileHash)
	if !hexShard6.MatchString(fileHash) {
		return "", fmt.Errorf("file hash must start with 6 lowercase hex characters, got %q", fileHash)
	}
	aa, bb, cc := fileHash[0:2], fileHash[2:4], fileHash[4:6]
	return filepath.ToSlash(filepath.Join(
		aa, bb, cc,
		fmt.Sprintf("%s_%s.jpg", fileHash, sizeName),
	)), nil
}

// joinUnderRoot joins rel to root and returns the absolute path. It rejects
// inputs containing NUL bytes, absolute paths, and any cleaned path that
// would escape root.
func joinUnderRoot(root, rel string) (string, error) {
	if strings.ContainsRune(rel, '\x00') {
		return "", errors.New("path contains NUL byte")
	}
	// Normalize slashes — accept both POSIX and OS-native separators on input.
	rel = filepath.FromSlash(rel)
	if rel == "" {
		return "", errors.New("relative path must not be empty")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("relative path must not be absolute: %q", rel)
	}

	joined := filepath.Join(root, rel)
	cleaned := filepath.Clean(joined)

	// Ensure the cleaned path is inside root. filepath.Rel handles the
	// trailing-slash and exact-match cases consistently.
	rootClean := filepath.Clean(root)
	relToRoot, err := filepath.Rel(rootClean, cleaned)
	if err != nil {
		return "", fmt.Errorf("path escapes root: %w", err)
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root %q", rel, rootClean)
	}
	if cleaned == rootClean {
		return "", errors.New("relative path must not equal root")
	}
	return cleaned, nil
}
