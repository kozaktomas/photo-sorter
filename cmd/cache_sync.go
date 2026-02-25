package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var cacheSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync face marker data from PhotoPrism to local cache",
	Long: `Sync face marker data from PhotoPrism to the local PostgreSQL cache.

This command refreshes the cached marker assignments (marker UID, subject UID,
subject name) for all faces in the database. Use this when faces have been
assigned or unassigned directly in PhotoPrism's native UI.

Examples:
  # Run sync with default concurrency (20 workers)
  photo-sorter cache sync

  # Limit concurrency
  photo-sorter cache sync --concurrency 5

  # JSON output for scripting
  photo-sorter cache sync --json`,
	RunE: runCacheSync,
}

func init() {
	cacheCmd.AddCommand(cacheSyncCmd)

	cacheSyncCmd.Flags().Int("concurrency", constants.WorkerPoolSize, "Number of parallel workers")
	cacheSyncCmd.Flags().Bool("json", false, "Output as JSON instead of progress bar")
}

// SyncCacheResult represents the result of a cache sync operation
type SyncCacheResult struct {
	Success       bool   `json:"success"`
	PhotosScanned int    `json:"photos_scanned"`
	FacesUpdated  int    `json:"faces_updated"`
	PhotosDeleted int    `json:"photos_deleted"`
	Errors        int    `json:"errors"`
	DurationMs    int64  `json:"duration_ms"`
	DurationHuman string `json:"duration_human,omitempty"`
}

// initSyncDeps initializes the database backends and PhotoPrism connection for cache sync.
type syncDeps struct {
	pp    *photoprism.PhotoPrism
	faceW database.FaceWriter
	embW  database.EmbeddingWriter
}

func initSyncDeps(ctx context.Context, cfg *config.Config, jsonOutput bool) (*syncDeps, error) {
	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	faceRepo := postgres.NewFaceRepository(pool)
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)
	database.RegisterEmbeddingWriter(func() database.EmbeddingWriter { return embeddingRepo })

	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism...")
	}
	pp, err := photoprism.NewPhotoPrismWithCapture(
		cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword(), captureDir,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}

	faceWriter, err := database.GetFaceWriter(ctx)
	if err != nil {
		pp.Logout()
		return nil, fmt.Errorf("failed to get face writer: %w", err)
	}

	embWriter, err := database.GetEmbeddingWriter(ctx)
	if err != nil {
		pp.Logout()
		return nil, fmt.Errorf("failed to get embedding writer: %w", err)
	}

	return &syncDeps{pp: pp, faceW: faceWriter, embW: embWriter}, nil
}

// collectSyncPhotoUIDs returns the union of photo UIDs from faces and embeddings tables.
func collectSyncPhotoUIDs(ctx context.Context, faceWriter database.FaceWriter, embWriter database.EmbeddingWriter) ([]string, error) {
	faceUIDs, err := faceWriter.GetUniquePhotoUIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get face photo UIDs: %w", err)
	}

	uidSet := make(map[string]struct{}, len(faceUIDs))
	for _, uid := range faceUIDs {
		uidSet[uid] = struct{}{}
	}

	embUIDs, err := embWriter.GetUniquePhotoUIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding photo UIDs: %w", err)
	}
	for _, uid := range embUIDs {
		uidSet[uid] = struct{}{}
	}

	photoUIDs := make([]string, 0, len(uidSet))
	for uid := range uidSet {
		photoUIDs = append(photoUIDs, uid)
	}
	return photoUIDs, nil
}

// processSyncPhotos runs the sync for all photos concurrently with progress tracking.
func processSyncPhotos(ctx context.Context, pp *photoprism.PhotoPrism, faceWriter database.FaceWriter, embWriter database.EmbeddingWriter, photoUIDs []string, concurrency int, bar *progressbar.ProgressBar) (int64, int64, int64) {
	var facesUpdated int64
	var photosDeleted int64
	var errorCount int64
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, photoUID := range photoUIDs {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			updated, deleted, err := syncPhotoCache(ctx, pp, faceWriter, embWriter, uid)
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
			} else {
				if updated > 0 {
					atomic.AddInt64(&facesUpdated, int64(updated))
				}
				if deleted {
					atomic.AddInt64(&photosDeleted, 1)
				}
			}

			if bar != nil {
				bar.Add(1)
			}
		}(photoUID)
	}

	wg.Wait()
	return facesUpdated, photosDeleted, errorCount
}

// newSyncProgressBar creates a progress bar for cache sync, or nil if JSON output.
func newSyncProgressBar(count int, jsonOutput bool) *progressbar.ProgressBar {
	if jsonOutput {
		return nil
	}
	return progressbar.NewOptions(count,
		progressbar.OptionSetDescription("Syncing cache"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)
}

// outputSyncResult outputs the sync result as JSON or human-readable.
func outputSyncResult(result SyncCacheResult, jsonOutput bool) error {
	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}

	fmt.Println("\nSync complete!")
	fmt.Printf("  Photos scanned: %d\n", result.PhotosScanned)
	fmt.Printf("  Faces updated:  %d\n", result.FacesUpdated)
	if result.PhotosDeleted > 0 {
		fmt.Printf("  Photos deleted: %d\n", result.PhotosDeleted)
	}
	if result.Errors > 0 {
		fmt.Printf("  Errors:         %d\n", result.Errors)
	}
	fmt.Printf("  Duration:       %s\n", result.DurationHuman)
	return nil
}

func runCacheSync(cmd *cobra.Command, args []string) error {
	concurrency := mustGetInt(cmd, "concurrency")
	jsonOutput := mustGetBool(cmd, "json")

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	deps, err := initSyncDeps(ctx, cfg, jsonOutput)
	if err != nil {
		return err
	}
	defer deps.pp.Logout()

	if !jsonOutput {
		fmt.Println("Fetching photo UIDs from database...")
	}
	photoUIDs, err := collectSyncPhotoUIDs(ctx, deps.faceW, deps.embW)
	if err != nil {
		return err
	}

	if len(photoUIDs) == 0 {
		return outputSyncResult(SyncCacheResult{Success: true, DurationMs: time.Since(startTime).Milliseconds()}, jsonOutput)
	}

	if !jsonOutput {
		fmt.Printf("Found %d photos with faces to sync\n\n", len(photoUIDs))
	}

	bar := newSyncProgressBar(len(photoUIDs), jsonOutput)
	facesUpdated, photosDeleted, errorCount := processSyncPhotos(ctx, deps.pp, deps.faceW, deps.embW, photoUIDs, concurrency, bar)
	if bar != nil {
		fmt.Println()
	}

	return outputSyncResult(SyncCacheResult{
		Success:       true,
		PhotosScanned: len(photoUIDs),
		FacesUpdated:  int(facesUpdated),
		PhotosDeleted: int(photosDeleted),
		Errors:        int(errorCount),
		DurationMs:    time.Since(startTime).Milliseconds(),
		DurationHuman: formatDuration(time.Since(startTime)),
	}, jsonOutput)
}

// cleanupDeletedPhoto removes all cached data for a deleted/archived photo.
func cleanupDeletedPhoto(ctx context.Context, faceWriter database.FaceWriter, embWriter database.EmbeddingWriter, photoUID string) {
	faceWriter.DeleteFacesByPhoto(ctx, photoUID)
	if embWriter != nil {
		embWriter.DeleteEmbedding(ctx, photoUID)
	}
}

// convertMarkersToInfos converts PhotoPrism markers to facematch.MarkerInfo slice.
func convertMarkersToInfos(markers []photoprism.Marker) []facematch.MarkerInfo {
	markerInfos := make([]facematch.MarkerInfo, 0, len(markers))
	for i := range markers {
		m := &markers[i]
		markerInfos = append(markerInfos, facematch.MarkerInfo{
			UID:     m.UID,
			Type:    m.Type,
			Name:    m.Name,
			SubjUID: m.SubjUID,
			X:       m.X,
			Y:       m.Y,
			W:       m.W,
			H:       m.H,
		})
	}
	return markerInfos
}

// matchFacesToMarkers matches each face to a marker and updates the cache.
// Returns the number of faces updated.
func matchFacesToMarkers(ctx context.Context, faceWriter database.FaceWriter, faces []database.StoredFace, markerInfos []facematch.MarkerInfo, fileInfo *facematch.PrimaryFileInfo, photoUID string) int {
	updated := 0
	for _, face := range faces {
		if len(face.BBox) != 4 {
			continue
		}
		match := facematch.MatchFaceToMarkers(face.BBox, markerInfos, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, constants.IoUThreshold)
		if match != nil {
			if face.MarkerUID != match.MarkerUID || face.SubjectUID != match.SubjectUID || face.SubjectName != match.SubjectName {
				faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, match.MarkerUID, match.SubjectUID, match.SubjectName)
				updated++
			}
		} else if face.MarkerUID != "" {
			faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, "", "", "")
			updated++
		}
	}
	return updated
}

// syncPhotoCache syncs the cache for a single photo and returns the number of faces updated,
// whether the photo was deleted/archived in PhotoPrism (404 or DeletedAt set), and any error.
// clearFaceAssignments clears marker assignments for all faces that have one.
func clearFaceAssignments(ctx context.Context, faceWriter database.FaceWriter, faces []database.StoredFace, photoUID string) int {
	updated := 0
	for _, face := range faces {
		if face.MarkerUID != "" {
			faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, "", "", "")
			updated++
		}
	}
	return updated
}

// isPhotoDeletedOrMissing checks if a photo is deleted or missing, and cleans up if so.
// Returns true if the photo was deleted/missing.
func isPhotoDeletedOrMissing(ctx context.Context, details map[string]any, err error, faceWriter database.FaceWriter, embWriter database.EmbeddingWriter, photoUID string) (bool, error) {
	if err != nil {
		if photoprism.IsNotFoundError(err) {
			cleanupDeletedPhoto(ctx, faceWriter, embWriter, photoUID)
			return true, nil
		}
		return false, fmt.Errorf("failed to get photo details: %w", err)
	}
	if photoprism.IsPhotoDeleted(details) {
		cleanupDeletedPhoto(ctx, faceWriter, embWriter, photoUID)
		return true, nil
	}
	return false, nil
}

func syncPhotoCache(ctx context.Context, pp *photoprism.PhotoPrism, faceWriter database.FaceWriter, embWriter database.EmbeddingWriter, photoUID string) (int, bool, error) {
	details, err := pp.GetPhotoDetails(photoUID)
	deleted, detailErr := isPhotoDeletedOrMissing(ctx, details, err, faceWriter, embWriter, photoUID)
	if detailErr != nil {
		return 0, false, detailErr
	}
	if deleted {
		return 0, true, nil
	}

	fileInfo := facematch.ExtractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return 0, false, nil
	}

	faceWriter.UpdateFacePhotoInfo(ctx, photoUID, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, fileInfo.UID)

	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil {
		return 0, false, fmt.Errorf("failed to get markers: %w", err)
	}

	faces, err := faceWriter.GetFaces(ctx, photoUID)
	if err != nil {
		return 0, false, fmt.Errorf("failed to get faces: %w", err)
	}

	if len(faces) == 0 {
		return 0, false, nil
	}

	if len(markers) == 0 {
		return clearFaceAssignments(ctx, faceWriter, faces, photoUID), false, nil
	}

	markerInfos := convertMarkersToInfos(markers)
	updated := matchFacesToMarkers(ctx, faceWriter, faces, markerInfos, fileInfo, photoUID)
	return updated, false, nil
}

// formatDuration formats a duration as a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
