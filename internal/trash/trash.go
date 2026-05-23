// Package trash implements the soft-delete trash workflow for native photos.
//
// Photos are archived (soft-deleted) by setting photos.archived_at = NOW().
// This package adds the remaining pieces: a hard-delete helper that removes
// a photo plus all of its derivatives (originals, thumbs, embeddings, faces,
// markers, phashes), a batch wrapper for the API handler, and an auto-purge
// daemon that hard-deletes anything older than the configured retention
// window.
//
// The package is kept dependency-light: it imports database (for the photo /
// embedding / face writers) and storage (for the on-disk originals and
// thumbnail cache). It deliberately does not import internal/web so the
// helpers can be reused by future CLI commands or background workers.
package trash

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// DefaultRetention is the default trash retention window. Photos archived
// for longer than this are eligible for auto-purge.
const DefaultRetention = 30 * 24 * time.Hour

// DefaultPurgeInterval is how often the auto-purge daemon scans the photos
// table for expired entries. Tunable but defaults to one hour to keep the
// background load minimal.
const DefaultPurgeInterval = time.Hour

// ErrNotArchived is returned by PurgePhoto when the target photo is not
// currently archived. It exists so callers can distinguish "skipped because
// the photo was live" from real I/O errors.
var ErrNotArchived = errors.New("photo is not archived")

// Store bundles the repositories and on-disk storage backing the trash. All
// fields are required: PurgePhoto and AutoPurge call into every one of them.
type Store struct {
	// Photos owns the photos table — reads (for the archive check), the
	// hard delete (which cascades to photo_files, photo_phashes, markers,
	// album_photos, photo_labels via FK), and ListArchivedBefore (for the
	// auto-purge daemon's eligibility query).
	Photos database.PhotoWriter
	// Embeddings owns the embeddings table. The embeddings row does not
	// cascade with the photos row (it predates the FK), so PurgePhoto has
	// to remove it explicitly.
	Embeddings database.EmbeddingWriter
	// Faces owns the faces / faces_processed cache. Like embeddings these
	// rows do not cascade with the photos row, so PurgePhoto deletes them
	// explicitly via DeleteFacesByPhoto.
	Faces database.FaceWriter
	// Files is the on-disk root that holds originals (under FilesRoot) and
	// thumbnails (under CacheRoot/thumb). PurgePhoto walks the photo_files
	// rows and storage.ValidThumbSizes to delete every derivative.
	Files *storage.Storage
}

// PurgePhoto hard-deletes one archived photo and every derivative tied to
// it. The order is:
//
//  1. Load the photo and verify it is archived (returns ErrNotArchived
//     otherwise).
//  2. Resolve the on-disk original paths via ListPhotoFiles + the photos.
//     file_path fallback and delete every original file from disk.
//  3. Delete every thumbnail under storage.ValidThumbSizes for the photo's
//     file_hash.
//  4. Delete the embedding row (no FK cascade) and faces / faces_processed
//     rows (no FK cascade).
//  5. Hard-delete the photo row, which cascades photo_files, photo_phashes,
//     markers, album_photos, photo_labels via ON DELETE CASCADE.
//
// Disk deletions are best-effort: missing files do not abort the purge so
// repeated runs converge on a fully-purged state even when an earlier
// attempt was interrupted halfway. Database deletions, by contrast, are
// fatal — the caller should retry on a future tick.
func PurgePhoto(ctx context.Context, uid string, s *Store) error {
	if err := validateStore(s); err != nil {
		return err
	}
	photo, err := s.Photos.GetPhoto(ctx, uid)
	if err != nil {
		return fmt.Errorf("get photo: %w", err)
	}
	if photo.ArchivedAt == nil {
		return ErrNotArchived
	}
	if err := purgePhotoFiles(ctx, s, photo); err != nil {
		return err
	}
	return purgePhotoDB(ctx, s, uid)
}

// validateStore reports the first missing dependency on the store, or nil
// when every field is set. Split out so PurgePhoto stays under the
// cyclomatic-complexity ceiling.
func validateStore(s *Store) error {
	if s == nil || s.Photos == nil || s.Embeddings == nil || s.Faces == nil || s.Files == nil {
		return errors.New("trash: store is missing required dependencies")
	}
	return nil
}

// purgePhotoFiles removes the on-disk originals and thumbnails for a
// photo. Best-effort: missing files do not return an error.
func purgePhotoFiles(ctx context.Context, s *Store, photo *database.Photo) error {
	if err := deleteOriginalsForPhoto(ctx, s, photo); err != nil {
		return fmt.Errorf("delete originals: %w", err)
	}
	if err := deleteThumbsForPhoto(s, photo.FileHash); err != nil {
		return fmt.Errorf("delete thumbs: %w", err)
	}
	return nil
}

// purgePhotoDB hard-deletes every database row tied to a photo: the
// embedding (no FK cascade), the faces (no FK cascade), and finally the
// photo row itself (cascades phashes, markers, photo_files,
// album_photos, photo_labels).
func purgePhotoDB(ctx context.Context, s *Store, uid string) error {
	if err := s.Embeddings.DeleteEmbedding(ctx, uid); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	if _, err := s.Faces.DeleteFacesByPhoto(ctx, uid); err != nil {
		return fmt.Errorf("delete faces: %w", err)
	}
	if err := s.Photos.DeletePhoto(ctx, uid); err != nil {
		return fmt.Errorf("delete photo row: %w", err)
	}
	return nil
}

// deleteOriginalsForPhoto removes every on-disk original file backing the
// given photo. It walks photo_files first (a single stack can hold multiple
// files — RAW + JPEG, edited variants) and falls back to the photo row's
// own file_path so single-file photos uploaded before photo_files was
// populated still get cleaned up. Missing files are not errors.
func deleteOriginalsForPhoto(
	ctx context.Context, s *Store, photo *database.Photo,
) error {
	files, err := s.Photos.ListPhotoFiles(ctx, photo.UID)
	if err != nil {
		return fmt.Errorf("list photo files: %w", err)
	}
	seen := make(map[string]struct{}, len(files)+1)
	for i := range files {
		path := files[i].FilePath
		if path == "" {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		if err := s.Files.DeleteOriginal(path); err != nil {
			return fmt.Errorf("delete original %q: %w", path, err)
		}
	}
	if photo.FilePath != "" {
		if _, dup := seen[photo.FilePath]; !dup {
			if err := s.Files.DeleteOriginal(photo.FilePath); err != nil {
				return fmt.Errorf("delete original %q: %w", photo.FilePath, err)
			}
		}
	}
	return nil
}

// deleteThumbsForPhoto removes every cached thumbnail for the given file
// hash. We do not consult the photo_files table here — thumbs are derived
// solely from the primary hash, so iterating storage.ValidThumbSizes is
// sufficient. A missing thumb (because the photo was never previewed at
// that size) is not an error.
func deleteThumbsForPhoto(s *Store, fileHash string) error {
	if fileHash == "" {
		return nil
	}
	for size := range storage.ValidThumbSizes {
		rel, err := storage.ThumbRelPath(fileHash, size)
		if err != nil {
			// A photo with a malformed hash (fewer than six hex chars)
			// cannot have any thumbs cached, so silently skip rather
			// than aborting the purge.
			continue
		}
		if err := s.Files.DeleteThumb(rel); err != nil {
			return fmt.Errorf("delete thumb %q: %w", rel, err)
		}
	}
	return nil
}

// AutoPurge finds every photo archived strictly before cutoff and hard-deletes
// it. The caller passes the cutoff so a daemon can use time.Now().Add(-retention)
// and a test can pass a fixed time. Returns (purged, errors) where purged is
// the count of fully-deleted photos and errors holds per-photo failures —
// the function never aborts the batch on a single failure so a poisoned
// photo does not block the rest of the trash from clearing.
func AutoPurge(
	ctx context.Context, cutoff time.Time, s *Store,
) (int, []error) {
	if s == nil || s.Photos == nil {
		return 0, []error{errors.New("trash: store is missing required dependencies")}
	}
	uids, err := s.Photos.ListArchivedBefore(ctx, cutoff)
	if err != nil {
		return 0, []error{fmt.Errorf("list archived: %w", err)}
	}
	purged := 0
	var errs []error
	for _, uid := range uids {
		if err := PurgePhoto(ctx, uid, s); err != nil {
			errs = append(errs, fmt.Errorf("purge %s: %w", uid, err))
			continue
		}
		purged++
	}
	return purged, errs
}

// RunDaemon runs an auto-purge loop until ctx is cancelled. Every interval
// it calls AutoPurge with cutoff = now - retention and logs the result. The
// first tick happens after interval (not immediately) so server startup is
// not slowed down. A zero or negative interval / retention falls back to
// the package defaults; passing a nil store is a programmer error and
// causes RunDaemon to return immediately after logging.
func RunDaemon(
	ctx context.Context, interval, retention time.Duration, s *Store,
) {
	if s == nil {
		log.Println("trash: RunDaemon called with nil store; auto-purge disabled")
		return
	}
	if interval <= 0 {
		interval = DefaultPurgeInterval
	}
	if retention <= 0 {
		retention = DefaultRetention
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("trash: auto-purge daemon started (interval=%s, retention=%s)",
		interval, retention)
	for {
		select {
		case <-ctx.Done():
			log.Println("trash: auto-purge daemon stopped")
			return
		case now := <-ticker.C:
			runPurgeTick(ctx, now, retention, s)
		}
	}
}

// runPurgeTick performs one auto-purge cycle with panic recovery. A panic
// inside AutoPurge (e.g. a nil-pointer deref in a writer implementation
// during a partial outage) must not kill the daemon — otherwise the
// background goroutine stops silently and trash accumulates until the next
// server restart. We recover, log, and let the ticker fire the next cycle.
func runPurgeTick(ctx context.Context, now time.Time, retention time.Duration, s *Store) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("trash: auto-purge tick panicked: %v", r)
		}
	}()
	cutoff := now.Add(-retention)
	purged, errs := AutoPurge(ctx, cutoff, s)
	if purged > 0 {
		log.Printf("trash: auto-purge removed %d photo(s) archived before %s",
			purged, cutoff.UTC().Format(time.RFC3339))
	}
	for _, err := range errs {
		log.Printf("trash: auto-purge: %v", err)
	}
}
