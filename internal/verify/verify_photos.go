package verify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// ppPhotoFile is the minimal projection of a PhotoPrism primary file the
// verifier needs to recompute SHA256 and look the row up in the sorter
// store.
type ppPhotoFile struct {
	PhotoUID string
	FileUID  string
	FileName string
	FileSize int64
}

// runPhotos compares photos one-to-one. For each PhotoPrism primary file
// we recompute SHA256 (skipping the file if size mismatch already tells
// us nothing matches), then look it up in the sorter via
// PhotoReader.GetPhotoByHash. The reverse pass walks the sorter and flags
// any photo whose hash has no PhotoPrism counterpart.
func (v *verifier) runPhotos(ctx context.Context) error {
	ppFiles, err := v.readPPPrimaryFiles(ctx)
	if err != nil {
		return fmt.Errorf("read pp primary files: %w", err)
	}
	v.report.Photos.PPCount = len(ppFiles)

	ppHashByPhotoUID, missing := v.hashAndMatchPhotos(ctx, ppFiles)
	v.report.Photos.MissingInSorter = truncate(missing)

	sorterCount, orphans, err := v.collectOrphanPhotos(ctx, ppHashByPhotoUID)
	if err != nil {
		return err
	}
	v.report.Photos.SorterCount = sorterCount
	v.report.Photos.OrphanInSorter = truncate(orphans)
	return nil
}

// hashResult bundles one worker's outcome — the recomputed SHA256, an
// ok flag indicating the file was present and the size matched, and an
// error reserved for genuine I/O failures.
type hashResult struct {
	ppUID string
	hash  string
	ok    bool
}

// hashAndMatchPhotos fans the PhotoPrism primary files out across a
// goroutine pool, recomputes SHA256 for each, looks the row up in the
// sorter, and returns:
//
//   - ppHashByPhotoUID: PhotoPrism photo_uid → SHA256 hash of every photo
//     that hashed successfully. Used by the orphan pass to recognise which
//     hashes belong to PhotoPrism.
//   - missing: PhotoPrism photo UIDs that have no sorter row.
//
// As a side effect the function populates v.photoMap (pp photo_uid →
// native photo UID) so later sections can resolve identities without
// rehashing.
func (v *verifier) hashAndMatchPhotos(
	ctx context.Context, files []ppPhotoFile,
) (map[string]string, []string) {
	results := v.runHashWorkers(ctx, files)
	ppHashByPhotoUID, missing := v.matchSorterByHash(ctx, results)
	for _, f := range files {
		if native, ok := v.photoMap[f.PhotoUID]; ok {
			v.fileMap[f.FileUID] = native
		}
	}
	sort.Strings(missing)
	return ppHashByPhotoUID, missing
}

// runHashWorkers drives the hash pool and returns the per-file results
// in arbitrary order. The pool size matches Options.Concurrency.
func (v *verifier) runHashWorkers(ctx context.Context, files []ppPhotoFile) []hashResult {
	concurrency := v.opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	jobs := make(chan ppPhotoFile)
	results := make(chan hashResult)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for f := range jobs {
				hash, ok, _ := v.computeHashForPPFile(f)
				results <- hashResult{ppUID: f.PhotoUID, hash: hash, ok: ok}
			}
		})
	}
	go func() {
		defer close(jobs)
		for _, f := range files {
			if ctx.Err() != nil {
				return
			}
			jobs <- f
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	out := make([]hashResult, 0, len(files))
	for r := range results {
		out = append(out, r)
	}
	return out
}

// matchSorterByHash looks every successfully-hashed PhotoPrism photo up
// in the sorter and records the PhotoPrism→native UID mapping when a
// row matches. The missing slice carries every PhotoPrism UID without a
// counterpart (hash failure, missing file, or absent sorter row).
func (v *verifier) matchSorterByHash(
	ctx context.Context, results []hashResult,
) (map[string]string, []string) {
	ppHashByPhotoUID := make(map[string]string, len(results))
	missing := make([]string, 0)
	for _, r := range results {
		if !r.ok {
			missing = append(missing, r.ppUID)
			continue
		}
		ppHashByPhotoUID[r.ppUID] = r.hash
		native, lookupErr := v.opts.Photos.GetPhotoByHash(ctx, r.hash)
		if lookupErr != nil || native == nil {
			if !errors.Is(lookupErr, database.ErrNotFound) && lookupErr != nil {
				// Lookup failed for a non-not-found reason; counting it
				// as missing keeps the report actionable.
				missing = append(missing, r.ppUID)
				continue
			}
			missing = append(missing, r.ppUID)
			continue
		}
		v.photoMap[r.ppUID] = native.UID
		v.nativeHashByPhotoUID[native.UID] = native.FileHash
	}
	return ppHashByPhotoUID, missing
}

// computeHashForPPFile stats the source file, requires its size to match
// the row's recorded size (size mismatch is enough to know nothing maps),
// and returns the recomputed SHA256. ok=false means the file is missing
// or sized differently; err is reserved for genuine I/O problems.
func (v *verifier) computeHashForPPFile(f ppPhotoFile) (string, bool, error) {
	src := filepath.Join(v.opts.OriginalsRoot, f.FileName)
	stat, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat %s: %w", src, err)
	}
	if f.FileSize > 0 && stat.Size() != f.FileSize {
		return "", false, nil
	}
	hash, err := storage.HashFile(src)
	if err != nil {
		return "", false, fmt.Errorf("hash %s: %w", src, err)
	}
	return hash, true, nil
}

// collectOrphanPhotos walks every photo in the sorter and reports those
// whose file_hash does not appear in the PhotoPrism set. ppHashes is the
// set of SHA256 hashes we know belong to PhotoPrism (built by
// hashAndMatchPhotos). Returns the sorter's photo count alongside the
// orphan list so the report row stays consistent.
func (v *verifier) collectOrphanPhotos(
	ctx context.Context, ppHashByPhotoUID map[string]string,
) (int, []string, error) {
	ppHashSet := make(map[string]struct{}, len(ppHashByPhotoUID))
	for _, h := range ppHashByPhotoUID {
		ppHashSet[h] = struct{}{}
	}

	const pageSize = 500
	offset := 0
	total := 0
	orphans := make([]string, 0)
	for {
		if err := ctx.Err(); err != nil {
			return total, orphans, fmt.Errorf("orphan walk canceled: %w", err)
		}
		page, _, err := v.opts.Photos.ListPhotos(ctx, database.PhotoFilter{
			SortBy: "newest",
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return total, orphans, fmt.Errorf("list sorter photos: %w", err)
		}
		if len(page) == 0 {
			break
		}
		for _, p := range page {
			total++
			if _, ok := ppHashSet[p.FileHash]; !ok {
				orphans = append(orphans, p.UID)
			}
		}
		offset += len(page)
	}
	sort.Strings(orphans)
	return total, orphans, nil
}

// readPPPrimaryFiles loads the projection of every primary file attached
// to a non-deleted PhotoPrism photo. file_size is optional in PhotoPrism;
// a zero value disables the size mismatch shortcut so the verifier still
// re-hashes the file.
func (v *verifier) readPPPrimaryFiles(ctx context.Context) ([]ppPhotoFile, error) {
	const query = `
		SELECT f.file_uid, p.photo_uid, f.file_name, COALESCE(f.file_size, 0)
		FROM files f
		JOIN photos p ON p.photo_uid = f.photo_uid
		WHERE COALESCE(f.file_primary, 0) = 1
		  AND f.deleted_at IS NULL
		  AND COALESCE(f.file_missing, 0) = 0
		  AND p.deleted_at IS NULL
		ORDER BY p.id`
	rows, err := v.opts.MariaDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query primary files: %w", err)
	}
	defer rows.Close()
	var out []ppPhotoFile
	for rows.Next() {
		var (
			f                              ppPhotoFile
			fileUID, photoUID, fileNameRaw []byte
			fileSize                       sql.NullInt64
		)
		if err := rows.Scan(&fileUID, &photoUID, &fileNameRaw, &fileSize); err != nil {
			return nil, fmt.Errorf("scan primary file: %w", err)
		}
		f.FileUID = string(fileUID)
		f.PhotoUID = string(photoUID)
		f.FileName = string(fileNameRaw)
		if fileSize.Valid {
			f.FileSize = fileSize.Int64
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate primary files: %w", err)
	}
	return out, nil
}
