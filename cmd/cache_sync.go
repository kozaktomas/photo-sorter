package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
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
	Errors        int    `json:"errors"`
	DurationMs    int64  `json:"duration_ms"`
	DurationHuman string `json:"duration_human,omitempty"`
}

func runCacheSync(cmd *cobra.Command, args []string) error {
	concurrency := mustGetInt(cmd, "concurrency")
	jsonOutput := mustGetBool(cmd, "json")

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	// Initialize PostgreSQL database
	if cfg.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	// Create singleton repositories and register with database package
	pool := postgres.GetGlobalPool()
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return nil },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	// Connect to PhotoPrism
	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism...")
	}
	pp, err := photoprism.NewPhotoPrismWithCapture(
		cfg.PhotoPrism.URL,
		cfg.PhotoPrism.Username,
		cfg.PhotoPrism.Password,
		captureDir,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Get face writer
	faceWriter, err := database.GetFaceWriter(ctx)
	if err != nil {
		return fmt.Errorf("failed to get face writer: %w", err)
	}

	// Get all unique photo UIDs with faces
	if !jsonOutput {
		fmt.Println("Fetching photo UIDs from database...")
	}
	photoUIDs, err := faceWriter.GetUniquePhotoUIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get photo UIDs: %w", err)
	}

	if len(photoUIDs) == 0 {
		result := SyncCacheResult{
			Success:       true,
			PhotosScanned: 0,
			FacesUpdated:  0,
			Errors:        0,
			DurationMs:    time.Since(startTime).Milliseconds(),
		}
		if jsonOutput {
			return outputJSON(result)
		}
		fmt.Println("No photos with faces found in database.")
		return nil
	}

	if !jsonOutput {
		fmt.Printf("Found %d photos with faces to sync\n\n", len(photoUIDs))
	}

	// Create progress bar (only for non-JSON output)
	var bar *progressbar.ProgressBar
	if !jsonOutput {
		bar = progressbar.NewOptions(len(photoUIDs),
			progressbar.OptionSetDescription("Syncing cache"),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetItsString("photos"),
			progressbar.OptionShowElapsedTimeOnFinish(),
			progressbar.OptionSetPredictTime(true),
			progressbar.OptionFullWidth(),
		)
	}

	// Process photos with concurrency
	var facesUpdated int64
	var errorCount int64
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, photoUID := range photoUIDs {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			updated, err := syncPhotoCache(ctx, pp, faceWriter, uid)
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
			} else if updated > 0 {
				atomic.AddInt64(&facesUpdated, int64(updated))
			}

			if bar != nil {
				bar.Add(1)
			}
		}(photoUID)
	}

	wg.Wait()

	if bar != nil {
		fmt.Println()
	}

	duration := time.Since(startTime)
	result := SyncCacheResult{
		Success:       true,
		PhotosScanned: len(photoUIDs),
		FacesUpdated:  int(facesUpdated),
		Errors:        int(errorCount),
		DurationMs:    duration.Milliseconds(),
		DurationHuman: formatDuration(duration),
	}

	if jsonOutput {
		// Remove human-readable duration for JSON output
		result.DurationHuman = ""
		return outputJSON(result)
	}

	// Human-readable output
	fmt.Println("\nSync complete!")
	fmt.Printf("  Photos scanned: %d\n", result.PhotosScanned)
	fmt.Printf("  Faces updated:  %d\n", result.FacesUpdated)
	if result.Errors > 0 {
		fmt.Printf("  Errors:         %d\n", result.Errors)
	}
	fmt.Printf("  Duration:       %s\n", result.DurationHuman)

	return nil
}

// syncPhotoCache syncs the cache for a single photo and returns the number of faces updated
func syncPhotoCache(ctx context.Context, pp *photoprism.PhotoPrism, faceWriter database.FaceWriter, photoUID string) (int, error) {
	// Get photo details for dimensions
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return 0, fmt.Errorf("failed to get photo details: %w", err)
	}

	fileInfo := facematch.ExtractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return 0, nil
	}

	// Update photo dimensions for all faces
	faceWriter.UpdateFacePhotoInfo(ctx, photoUID, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, fileInfo.UID)

	// Get markers from PhotoPrism
	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil {
		return 0, fmt.Errorf("failed to get markers: %w", err)
	}

	// Get faces for this photo from database
	faces, err := faceWriter.GetFaces(ctx, photoUID)
	if err != nil {
		return 0, fmt.Errorf("failed to get faces: %w", err)
	}

	if len(faces) == 0 {
		return 0, nil
	}

	// If no markers, clear any existing marker assignments
	if len(markers) == 0 {
		updated := 0
		for _, face := range faces {
			if face.MarkerUID != "" {
				faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, "", "", "")
				updated++
			}
		}
		return updated, nil
	}

	// Convert markers to facematch.MarkerInfo
	markerInfos := make([]facematch.MarkerInfo, 0, len(markers))
	for _, m := range markers {
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

	// Match each face to a marker and update cache
	updated := 0
	for _, face := range faces {
		if len(face.BBox) != 4 {
			continue
		}
		match := facematch.MatchFaceToMarkers(face.BBox, markerInfos, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, constants.IoUThreshold)
		if match != nil {
			// Check if update is needed (to count actual changes)
			if face.MarkerUID != match.MarkerUID || face.SubjectUID != match.SubjectUID || face.SubjectName != match.SubjectName {
				faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, match.MarkerUID, match.SubjectUID, match.SubjectName)
				updated++
			}
		} else if face.MarkerUID != "" {
			// No match found but face had a marker - clear it
			faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, "", "", "")
			updated++
		}
	}

	return updated, nil
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

// outputJSON outputs data as pretty-printed JSON
func outputCacheSyncJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
