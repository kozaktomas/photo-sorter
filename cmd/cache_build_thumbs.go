package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/imgconvert"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// cacheBuildThumbsDefaultConcurrency is the default worker count. Thumbnail
// generation is CPU-bound (JPEG decode + resize + encode); the Pi has 4
// cores so 4 workers saturates the device without thrashing.
const cacheBuildThumbsDefaultConcurrency = 4

// buildThumbsPageSize is the page size used when paginating photos via
// ListPhotos. It is below maxPhotoListLimit so the call always succeeds.
const buildThumbsPageSize = 200

var cacheBuildThumbsCmd = &cobra.Command{
	Use:   "build-thumbs",
	Short: "Generate missing thumbnails for every photo in the database",
	Long: `Walk every photo in the database and generate any missing thumbnails
under the cache root. Used after migration, after a cache wipe, or whenever
new size definitions are added.

For each photo the original is resolved via storage, HEIC/RAW originals are
funnelled through imgconvert.EnsureDecodable (heif-convert / dcraw), and
thumb.GenerateSizes writes the requested thumbnail subset. Missing thumbs
are detected per-size, so the command is fully idempotent — re-running on
a fully cached library is essentially a no-op.

Examples:
  # Backfill every missing thumbnail (default concurrency, every size)
  photo-sorter cache build-thumbs

  # Regenerate one photo's thumbs (handy after restoring an original)
  photo-sorter cache build-thumbs --photo-uid p123abc

  # Only the sizes that the web UI serves
  photo-sorter cache build-thumbs --sizes fit_720,fit_1920,tile_224

  # Limit the run when smoke-testing a fresh migration
  photo-sorter cache build-thumbs --limit 50`,
	RunE: runCacheBuildThumbs,
}

func init() {
	cacheCmd.AddCommand(cacheBuildThumbsCmd)

	cacheBuildThumbsCmd.Flags().Int("concurrency", cacheBuildThumbsDefaultConcurrency,
		"Number of parallel workers")
	cacheBuildThumbsCmd.Flags().StringSlice("sizes", nil,
		"Comma-separated list of thumb sizes to generate (default: every registered size)")
	cacheBuildThumbsCmd.Flags().Int("limit", 0,
		"Maximum number of photos to process (0 = unlimited)")
	cacheBuildThumbsCmd.Flags().Bool("only-missing", true,
		"Only generate missing thumbnails (default true). Pass --only-missing=false to force regeneration.")
	cacheBuildThumbsCmd.Flags().String("photo-uid", "",
		"Regenerate thumbs for a single photo by UID")
	cacheBuildThumbsCmd.Flags().Bool("json", false,
		"Output result as JSON instead of a progress bar")
}

// BuildThumbsResult summarises a cache build-thumbs run.
type BuildThumbsResult struct {
	Success       bool   `json:"success"`
	PhotosScanned int    `json:"photos_scanned"`
	Generated     int    `json:"generated"`
	Skipped       int    `json:"skipped"`
	Failed        int    `json:"failed"`
	DurationMs    int64  `json:"duration_ms"`
	DurationHuman string `json:"duration_human,omitempty"`
}

// buildThumbsDeps bundles the storage + photo reader the backfill workers
// need so the worker pool can capture references without touching the
// global provider in the hot loop.
type buildThumbsDeps struct {
	photoReader database.PhotoReader
	store       *storage.Storage
	sizes       []string
	onlyMissing bool
}

// initBuildThumbsDeps initialises PostgreSQL + storage and returns the
// dependencies the worker pool will use. The repos are registered on the
// global provider so future callers in the same process pick them up.
func initBuildThumbsDeps(_ context.Context, cfg *config.Config) (*buildThumbsDeps, error) {
	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("initialise PostgreSQL: %w", err)
	}
	pool := postgres.GetGlobalPool()

	photoRepo := postgres.NewPhotoRepository(pool)
	database.RegisterPhotoWriter(func() database.PhotoWriter { return photoRepo })

	store, err := storage.New(cfg.Storage.OriginalsPath, cfg.Storage.CachePath)
	if err != nil {
		return nil, fmt.Errorf("storage init: %w", err)
	}

	return &buildThumbsDeps{
		photoReader: photoRepo,
		store:       store,
	}, nil
}

// resolveSizes parses the --sizes flag value into a validated slice of
// size names. An empty input expands to every registered size; unknown
// names yield an error so typos do not silently no-op.
func resolveSizes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return thumb.SizeNames(), nil
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !thumb.IsValidSize(s) {
			return nil, fmt.Errorf("unknown thumbnail size %q", s)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return thumb.SizeNames(), nil
	}
	return out, nil
}

func runCacheBuildThumbs(cmd *cobra.Command, _ []string) error {
	concurrency := max(mustGetInt(cmd, "concurrency"), 1)
	limit := mustGetInt(cmd, "limit")
	onlyMissing := mustGetBool(cmd, "only-missing")
	photoUID := mustGetString(cmd, "photo-uid")
	jsonOutput := mustGetBool(cmd, "json")
	rawSizes := mustGetStringSlice(cmd, "sizes")

	sizes, err := resolveSizes(rawSizes)
	if err != nil {
		return err
	}

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	deps, err := initBuildThumbsDeps(ctx, cfg)
	if err != nil {
		return err
	}
	deps.sizes = sizes
	deps.onlyMissing = onlyMissing

	uids, err := collectPhotoUIDsForThumbs(ctx, deps.photoReader, photoUID, limit, jsonOutput)
	if err != nil {
		return err
	}
	if len(uids) == 0 {
		return outputBuildThumbsResult(BuildThumbsResult{
			Success:       true,
			DurationMs:    time.Since(startTime).Milliseconds(),
			DurationHuman: formatDuration(time.Since(startTime)),
		}, jsonOutput)
	}
	if !jsonOutput {
		fmt.Printf("Processing %d photos across %d size(s)\n\n", len(uids), len(sizes))
	}

	bar := newBuildThumbsProgressBar(len(uids), jsonOutput)
	generated, skipped, failed := buildThumbsForPhotos(ctx, deps, uids, concurrency, bar)
	if bar != nil {
		fmt.Println()
	}

	return outputBuildThumbsResult(BuildThumbsResult{
		Success:       true,
		PhotosScanned: len(uids),
		Generated:     int(generated),
		Skipped:       int(skipped),
		Failed:        int(failed),
		DurationMs:    time.Since(startTime).Milliseconds(),
		DurationHuman: formatDuration(time.Since(startTime)),
	}, jsonOutput)
}

// collectPhotoUIDsForThumbs returns the photo UIDs the backfill should
// process. When photoUID is non-empty the result is exactly that one UID
// (verifying the row exists first); otherwise the photos table is paged
// through in stable order until limit (0 = unlimited) is reached.
func collectPhotoUIDsForThumbs(
	ctx context.Context, reader database.PhotoReader,
	photoUID string, limit int, jsonOutput bool,
) ([]string, error) {
	if photoUID != "" {
		if _, err := reader.GetPhoto(ctx, photoUID); err != nil {
			return nil, fmt.Errorf("photo %q: %w", photoUID, err)
		}
		return []string{photoUID}, nil
	}

	if !jsonOutput {
		fmt.Println("Listing photos...")
	}
	var uids []string
	offset := 0
	for {
		page, _, err := reader.ListPhotos(ctx, database.PhotoFilter{
			SortBy: "newest",
			Limit:  buildThumbsPageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list photos: %w", err)
		}
		if len(page) == 0 {
			break
		}
		for _, p := range page {
			uids = append(uids, p.UID)
			if limit > 0 && len(uids) >= limit {
				return uids, nil
			}
		}
		offset += len(page)
	}
	return uids, nil
}

// buildThumbsForPhotos fans the supplied photo UIDs out across the worker
// pool and returns (generated, skipped, failed) counts. The progress bar
// is advanced once per photo regardless of outcome.
func buildThumbsForPhotos(
	ctx context.Context, deps *buildThumbsDeps, uids []string,
	concurrency int, bar *progressbar.ProgressBar,
) (int64, int64, int64) {
	var generated, skipped, failed int64
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, uid := range uids {
		wg.Add(1)
		go func(photoUID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			outcome := backfillSinglePhotoThumbs(ctx, deps, photoUID)
			switch outcome.status {
			case thumbBuildGenerated:
				atomic.AddInt64(&generated, int64(outcome.wrote))
			case thumbBuildSkipped:
				atomic.AddInt64(&skipped, 1)
			case thumbBuildError:
				atomic.AddInt64(&failed, 1)
				if outcome.err != nil {
					fmt.Fprintf(os.Stderr, "build-thumbs: %s: %v\n", photoUID, outcome.err)
				}
			}

			if bar != nil {
				bar.Add(1)
			}
		}(uid)
	}
	wg.Wait()
	return generated, skipped, failed
}

// thumbBuildStatus is the per-photo outcome of backfillSinglePhotoThumbs.
type thumbBuildStatus int

const (
	thumbBuildGenerated thumbBuildStatus = iota
	thumbBuildSkipped
	thumbBuildError
)

// thumbBuildOutcome captures both the high-level status and the number of
// thumbnails actually written, so the summary can report a per-thumbnail
// generated count rather than per-photo.
type thumbBuildOutcome struct {
	status thumbBuildStatus
	wrote  int
	err    error
}

// backfillSinglePhotoThumbs resolves the photo's original, decodes it via
// imgconvert.EnsureDecodable (so HEIC/RAW originals are funnelled through
// heif-convert/dcraw), and writes the requested thumbnail subset via
// thumb.GenerateSizes. When deps.onlyMissing is false, any existing thumbs
// for the requested sizes are deleted first so the regen actually rewrites
// the files.
func backfillSinglePhotoThumbs(
	ctx context.Context, deps *buildThumbsDeps, photoUID string,
) thumbBuildOutcome {
	photo, err := deps.photoReader.GetPhoto(ctx, photoUID)
	if err != nil {
		return thumbBuildOutcome{status: thumbBuildError, err: fmt.Errorf("get photo: %w", err)}
	}
	absPath, err := deps.store.AbsOriginal(photo.FilePath)
	if err != nil {
		return thumbBuildOutcome{status: thumbBuildError, err: fmt.Errorf("resolve original: %w", err)}
	}
	if _, statErr := os.Stat(absPath); statErr != nil {
		return thumbBuildOutcome{status: thumbBuildError, err: fmt.Errorf("stat original: %w", statErr)}
	}

	if !deps.onlyMissing {
		for _, name := range deps.sizes {
			rel, relErr := storage.ThumbRelPath(photo.FileHash, name)
			if relErr != nil {
				continue
			}
			_ = deps.store.DeleteThumb(rel)
		}
	}

	missingBefore := countMissingThumbs(deps.store, photo.FileHash, deps.sizes)
	if missingBefore == 0 {
		return thumbBuildOutcome{status: thumbBuildSkipped}
	}

	decodable, cleanup, err := imgconvert.EnsureDecodable(ctx, absPath)
	if err != nil {
		return thumbBuildOutcome{status: thumbBuildError, err: fmt.Errorf("ensure decodable: %w", err)}
	}
	defer cleanup()

	src := thumb.Source{Path: decodable, Orientation: photo.FileOrientation}
	if _, err := thumb.GenerateSizes(src, deps.sizes, deps.store, photo.FileHash); err != nil {
		return thumbBuildOutcome{status: thumbBuildError, err: fmt.Errorf("generate thumbs: %w", err)}
	}
	return thumbBuildOutcome{status: thumbBuildGenerated, wrote: missingBefore}
}

// countMissingThumbs returns the number of requested sizes whose thumbnail
// file is not yet present in the cache. Used both to skip photos where
// everything is already cached and to count the per-photo "generated"
// number after a successful run.
func countMissingThumbs(store *storage.Storage, fileHash string, sizes []string) int {
	missing := 0
	for _, name := range sizes {
		rel, err := storage.ThumbRelPath(fileHash, name)
		if err != nil {
			continue
		}
		if !store.ThumbExists(rel) {
			missing++
		}
	}
	return missing
}

func newBuildThumbsProgressBar(total int, jsonOutput bool) *progressbar.ProgressBar {
	if jsonOutput {
		return nil
	}
	return progressbar.NewOptions(total,
		progressbar.OptionSetDescription("Building thumbs"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)
}

// outputBuildThumbsResult writes the run summary as either JSON or a short
// human-readable block, mirroring the format used by the other cache
// subcommands.
func outputBuildThumbsResult(result BuildThumbsResult, jsonOutput bool) error {
	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}
	fmt.Println("Backfill complete!")
	fmt.Printf("  Photos scanned: %d\n", result.PhotosScanned)
	fmt.Printf("  Generated:      %d\n", result.Generated)
	if result.Skipped > 0 {
		fmt.Printf("  Skipped:        %d\n", result.Skipped)
	}
	if result.Failed > 0 {
		fmt.Printf("  Failed:         %d\n", result.Failed)
	}
	fmt.Printf("  Duration:       %s\n", result.DurationHuman)
	return nil
}
