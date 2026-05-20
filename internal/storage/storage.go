// Package storage owns the on-disk layout for photo originals and the
// thumbnail cache. It mirrors the PhotoPrism convention so an existing
// PhotoPrism library can be migrated in place without renaming files.
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Storage manages the originals and thumbnail cache directories.
type Storage struct {
	originalsRoot string
	cacheRoot     string
}

// New constructs a Storage rooted at originalsRoot and cacheRoot. Both
// directories must be absolute and are created (with parents) if they don't
// exist. The thumb sub-tree (cacheRoot/thumb) is created eagerly as well.
func New(originalsRoot, cacheRoot string) (*Storage, error) {
	if originalsRoot == "" {
		return nil, errors.New("originalsRoot must not be empty")
	}
	if cacheRoot == "" {
		return nil, errors.New("cacheRoot must not be empty")
	}

	absOriginals, err := filepath.Abs(originalsRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve originalsRoot: %w", err)
	}
	absCache, err := filepath.Abs(cacheRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve cacheRoot: %w", err)
	}

	// Spec mandates 0o755 to match the PhotoPrism on-disk layout so a
	// migration can simply rsync existing trees in place.
	//nolint:gosec // G301: 0o755 matches PhotoPrism layout
	if err := os.MkdirAll(absOriginals, 0o755); err != nil {
		return nil, fmt.Errorf("create originalsRoot %q: %w", absOriginals, err)
	}
	thumbRoot := filepath.Join(absCache, "thumb")
	//nolint:gosec // G301: 0o755 matches PhotoPrism layout
	if err := os.MkdirAll(thumbRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create cacheRoot %q: %w", absCache, err)
	}

	return &Storage{
		originalsRoot: absOriginals,
		cacheRoot:     absCache,
	}, nil
}

// OriginalsRoot returns the absolute path of the originals root directory.
func (s *Storage) OriginalsRoot() string { return s.originalsRoot }

// CacheRoot returns the absolute path of the cache root directory.
func (s *Storage) CacheRoot() string { return s.cacheRoot }

// AbsOriginal returns the absolute path for a relative original path. The
// result is guaranteed to live under OriginalsRoot.
func (s *Storage) AbsOriginal(rel string) (string, error) {
	return joinUnderRoot(s.originalsRoot, rel)
}

// AbsThumb returns the absolute path for a relative thumbnail path. Thumbnails
// live under <cacheRoot>/thumb/.
func (s *Storage) AbsThumb(rel string) (string, error) {
	return joinUnderRoot(filepath.Join(s.cacheRoot, "thumb"), rel)
}

// OriginalExists reports whether the original at rel exists.
func (s *Storage) OriginalExists(rel string) bool {
	abs, err := s.AbsOriginal(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}

// ThumbExists reports whether the thumbnail at rel exists.
func (s *Storage) ThumbExists(rel string) bool {
	abs, err := s.AbsThumb(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}

// OpenOriginal opens the original at rel for reading.
func (s *Storage) OpenOriginal(rel string) (*os.File, error) {
	abs, err := s.AbsOriginal(rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs) // #nosec G304 -- abs is validated by AbsOriginal to live under originalsRoot
	if err != nil {
		return nil, fmt.Errorf("open original %q: %w", rel, err)
	}
	return f, nil
}

// OpenThumb opens the thumbnail at rel for reading.
func (s *Storage) OpenThumb(rel string) (*os.File, error) {
	abs, err := s.AbsThumb(rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs) // #nosec G304 -- abs is validated by AbsThumb to live under cacheRoot/thumb
	if err != nil {
		return nil, fmt.Errorf("open thumb %q: %w", rel, err)
	}
	return f, nil
}

// DeleteOriginal removes the original at rel. Returns nil if the file does
// not exist.
func (s *Storage) DeleteOriginal(rel string) error {
	abs, err := s.AbsOriginal(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete original %q: %w", rel, err)
	}
	return nil
}

// DeleteThumb removes the thumbnail at rel. Returns nil if the file does not
// exist.
func (s *Storage) DeleteThumb(rel string) error {
	abs, err := s.AbsThumb(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete thumb %q: %w", rel, err)
	}
	return nil
}
