package verify

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// runDisk walks the sorter's originals tree and reports any file that
// has no `photos.file_path` row pointing at it. The walk treats every
// regular file as a candidate; symlinks, dotfiles and temp/.tmp files
// are ignored so day-to-day churn does not produce noise.
func (v *verifier) runDisk(ctx context.Context) error {
	knownPaths, err := v.collectKnownFilePaths(ctx)
	if err != nil {
		return err
	}

	root := v.opts.Store.OriginalsRoot()
	orphans, err := walkOriginalsForOrphans(root, knownPaths)
	if err != nil {
		return fmt.Errorf("walk originals root: %w", err)
	}
	sort.Strings(orphans)
	v.report.Disk.OrphanFiles = truncate(orphans)
	return nil
}

// walkOriginalsForOrphans walks root and returns every regular file that
// is not in the known-paths set. Hidden files and .tmp scratch files
// from the atomic writer are skipped so day-to-day churn does not
// pollute the report.
func walkOriginalsForOrphans(root string, known map[string]struct{}) ([]string, error) {
	var orphans []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if shouldSkipDiskEntry(d) {
			return nil
		}
		rel, ok := relativeOrSkip(root, path)
		if !ok {
			return nil
		}
		if _, isKnown := known[rel]; isKnown {
			return nil
		}
		orphans = append(orphans, rel)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walkdir: %w", walkErr)
	}
	return orphans, nil
}

// shouldSkipDiskEntry returns true when a WalkDir entry should be
// skipped by the orphan scan: directories, dotfiles, and the atomic
// writer's .tmp scratch files all fall through.
func shouldSkipDiskEntry(d fs.DirEntry) bool {
	if d.IsDir() {
		return true
	}
	base := d.Name()
	if base == "" || base[0] == '.' {
		return true
	}
	if strings.HasSuffix(base, ".tmp") || strings.Contains(base, ".tmp.") {
		return true
	}
	return false
}

// relativeOrSkip returns the forward-slash relative path of abs under
// root, or ok=false when filepath.Rel fails (which we treat as a non-fatal
// "skip this entry" so the walk continues).
func relativeOrSkip(root, abs string) (string, bool) {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// collectKnownFilePaths returns the set of relative paths recorded in
// the photos table (and the photo_files sidecar table). Paths are
// normalised to forward slashes so the on-disk walk's filepath.Rel
// output compares equal regardless of host OS.
func (v *verifier) collectKnownFilePaths(ctx context.Context) (map[string]struct{}, error) {
	known := make(map[string]struct{})
	const pageSize = 500
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return known, fmt.Errorf("path collection canceled: %w", err)
		}
		page, _, err := v.opts.Photos.ListPhotos(ctx, database.PhotoFilter{
			Limit:  pageSize,
			Offset: offset,
			SortBy: "newest",
		})
		if err != nil {
			return known, fmt.Errorf("list photos for path scan: %w", err)
		}
		if len(page) == 0 {
			break
		}
		v.appendKnownPathsFromPage(ctx, page, known)
		offset += len(page)
	}
	return known, nil
}

// appendKnownPathsFromPage adds the primary file_path and every
// photo_files row's path to the known-paths set. ListPhotoFiles errors
// are swallowed; the worst case is one missed sidecar producing a
// spurious orphan in the report, which the operator can resolve by
// re-running once the underlying lookup recovers.
func (v *verifier) appendKnownPathsFromPage(
	ctx context.Context, page []database.Photo, known map[string]struct{},
) {
	for _, p := range page {
		if p.FilePath != "" {
			known[filepath.ToSlash(p.FilePath)] = struct{}{}
		}
		files, err := v.opts.Photos.ListPhotoFiles(ctx, p.UID)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.FilePath != "" {
				known[filepath.ToSlash(f.FilePath)] = struct{}{}
			}
		}
	}
}
