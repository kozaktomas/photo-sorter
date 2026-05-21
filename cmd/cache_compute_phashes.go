package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/imgconvert"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var cacheComputePHashesCmd = &cobra.Command{
	Use:   "compute-phashes",
	Short: "Backfill perceptual hashes for photos that lack them",
	Long: `Walk the photos table and populate photo_phashes for any row that
does not yet have an entry. Idempotent: re-runs only re-hash photos that
were added since the last invocation.

Examples:
  # Backfill every photo missing a pHash (default concurrency)
  photo-sorter cache compute-phashes

  # Limit the number of rows processed per run
  photo-sorter cache compute-phashes --limit 1000

  # Increase parallelism (decode + hash are CPU-bound)
  photo-sorter cache compute-phashes --concurrency 8

  # JSON output for scripting
  photo-sorter cache compute-phashes --json`,
	RunE: runCacheComputePHashes,
}

const (
	// cachePHashDefaultConcurrency is the default number of worker
	// goroutines decoding + hashing photos in parallel. Decoding a JPEG
	// and running the DCT is CPU-bound; the Pi has 4 cores so 4 workers
	// saturates the box without thrashing.
	cachePHashDefaultConcurrency = 4
)

func init() {
	cacheCmd.AddCommand(cacheComputePHashesCmd)

	cacheComputePHashesCmd.Flags().Int("limit", 0, "Maximum number of photos to process (0 = unlimited)")
	cacheComputePHashesCmd.Flags().Int("concurrency", cachePHashDefaultConcurrency, "Number of parallel workers")
	cacheComputePHashesCmd.Flags().Bool("json", false, "Output as JSON instead of a progress bar")
}

// ComputePHashesResult summarises a cache compute-phashes run. Returned to
// the user as JSON when --json is set, and rendered as a human-readable
// summary otherwise. Field names match the result-format convention of
// the other cache subcommands (see SyncCacheResult in cache_sync.go).
type ComputePHashesResult struct {
	Success       bool   `json:"success"`
	PhotosScanned int    `json:"photos_scanned"`
	HashesWritten int    `json:"hashes_written"`
	Errors        int    `json:"errors"`
	Skipped       int    `json:"skipped"`
	DurationMs    int64  `json:"duration_ms"`
	DurationHuman string `json:"duration_human,omitempty"`
}

// phashJobDeps bundles the writers/readers the backfill workers need; it
// is built once at the top of the command so each goroutine can capture
// references without hitting the global provider for every photo.
type phashJobDeps struct {
	phashWriter database.PHashWriter
	photoReader database.PhotoReader
	store       *storage.Storage
}

// initPHashDeps initialises PostgreSQL + storage and returns the deps the
// workers will use. The PHashRepository is registered so future callers
// inside the same process pick it up via the global provider.
func initPHashDeps(_ context.Context, cfg *config.Config) (*phashJobDeps, error) {
	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("initialise PostgreSQL: %w", err)
	}
	pool := postgres.GetGlobalPool()

	phashRepo := postgres.NewPHashRepository(pool)
	database.RegisterPHashWriter(func() database.PHashWriter { return phashRepo })

	photoRepo := postgres.NewPhotoRepository(pool)
	database.RegisterPhotoWriter(func() database.PhotoWriter { return photoRepo })

	store, err := storage.New(cfg.Storage.OriginalsPath, cfg.Storage.CachePath)
	if err != nil {
		return nil, fmt.Errorf("storage init: %w", err)
	}

	return &phashJobDeps{
		phashWriter: phashRepo,
		photoReader: photoRepo,
		store:       store,
	}, nil
}

func runCacheComputePHashes(cmd *cobra.Command, _ []string) error {
	limit := mustGetInt(cmd, "limit")
	concurrency := mustGetInt(cmd, "concurrency")
	jsonOutput := mustGetBool(cmd, "json")

	if concurrency < 1 {
		concurrency = 1
	}

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	deps, err := initPHashDeps(ctx, cfg)
	if err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Println("Fetching photos without pHash...")
	}
	uids, err := deps.phashWriter.ListPhotosWithoutPHash(ctx, limit)
	if err != nil {
		return fmt.Errorf("list missing phashes: %w", err)
	}

	if len(uids) == 0 {
		return outputComputePHashResult(ComputePHashesResult{
			Success:       true,
			DurationMs:    time.Since(startTime).Milliseconds(),
			DurationHuman: formatDuration(time.Since(startTime)),
		}, jsonOutput)
	}
	if !jsonOutput {
		fmt.Printf("Found %d photos needing pHash backfill\n\n", len(uids))
	}

	bar := newPHashProgressBar(len(uids), jsonOutput)
	hashed, skipped, errs := computePHashesForPhotos(ctx, deps, uids, concurrency, bar)
	if bar != nil {
		fmt.Println()
	}

	return outputComputePHashResult(ComputePHashesResult{
		Success:       true,
		PhotosScanned: len(uids),
		HashesWritten: int(hashed),
		Skipped:       int(skipped),
		Errors:        int(errs),
		DurationMs:    time.Since(startTime).Milliseconds(),
		DurationHuman: formatDuration(time.Since(startTime)),
	}, jsonOutput)
}

// computePHashesForPhotos fans the supplied photo UIDs out across the
// worker pool and returns counts of (hashed, skipped, errored) rows.
// Skipped covers photos that exist in the photos table but whose primary
// file is unreadable or in a format the fingerprint package cannot decode.
func computePHashesForPhotos(
	ctx context.Context,
	deps *phashJobDeps,
	uids []string,
	concurrency int,
	bar *progressbar.ProgressBar,
) (int64, int64, int64) {
	var hashed, skipped, errs int64
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, uid := range uids {
		wg.Add(1)
		go func(photoUID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			status := backfillSinglePHash(ctx, deps, photoUID)
			switch status {
			case phashBackfillHashed:
				atomic.AddInt64(&hashed, 1)
			case phashBackfillSkipped:
				atomic.AddInt64(&skipped, 1)
			case phashBackfillError:
				atomic.AddInt64(&errs, 1)
			}

			if bar != nil {
				bar.Add(1)
			}
		}(uid)
	}
	wg.Wait()
	return hashed, skipped, errs
}

// phashBackfillStatus is the per-photo outcome of backfillSinglePHash.
type phashBackfillStatus int

const (
	phashBackfillHashed phashBackfillStatus = iota
	phashBackfillSkipped
	phashBackfillError
)

// backfillSinglePHash resolves the photo's primary file, decodes it via
// imgconvert.EnsureDecodable (so HEIC/RAW originals are funnelled through
// heif-convert/dcraw), computes pHash + dHash, and upserts the row.
// Returns one of the phashBackfill* statuses so the caller can update
// summary counters.
func backfillSinglePHash(ctx context.Context, deps *phashJobDeps, photoUID string) phashBackfillStatus {
	photo, err := deps.photoReader.GetPhoto(ctx, photoUID)
	if err != nil {
		return phashBackfillSkipped
	}
	absPath, err := deps.store.AbsOriginal(photo.FilePath)
	if err != nil {
		return phashBackfillSkipped
	}
	if _, statErr := os.Stat(absPath); statErr != nil {
		return phashBackfillSkipped
	}
	decodable, cleanup, err := imgconvert.EnsureDecodable(ctx, absPath)
	if err != nil {
		return phashBackfillSkipped
	}
	defer cleanup()

	data, err := os.ReadFile(filepath.Clean(decodable))
	if err != nil {
		return phashBackfillError
	}
	hashes, err := fingerprint.ComputeHashes(data)
	if err != nil {
		return phashBackfillSkipped
	}
	if err := deps.phashWriter.SavePHash(ctx, photo.UID, hashes.PHashBits, hashes.DHashBits); err != nil {
		return phashBackfillError
	}
	return phashBackfillHashed
}

func newPHashProgressBar(total int, jsonOutput bool) *progressbar.ProgressBar {
	if jsonOutput {
		return nil
	}
	return progressbar.NewOptions(total,
		progressbar.OptionSetDescription("Hashing photos"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)
}

// outputComputePHashResult writes the run summary as either JSON or a
// short human-readable block, mirroring the format used by the other
// cache subcommands.
func outputComputePHashResult(result ComputePHashesResult, jsonOutput bool) error {
	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}
	fmt.Println("Backfill complete!")
	fmt.Printf("  Photos scanned: %d\n", result.PhotosScanned)
	fmt.Printf("  Hashes written: %d\n", result.HashesWritten)
	if result.Skipped > 0 {
		fmt.Printf("  Skipped:        %d\n", result.Skipped)
	}
	if result.Errors > 0 {
		fmt.Printf("  Errors:         %d\n", result.Errors)
	}
	fmt.Printf("  Duration:       %s\n", result.DurationHuman)
	return nil
}
