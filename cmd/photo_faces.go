package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/tomas/photo-sorter/internal/config"
	"github.com/tomas/photo-sorter/internal/database"
	"github.com/tomas/photo-sorter/internal/fingerprint"
	"github.com/tomas/photo-sorter/internal/photoprism"
)

var photoFacesCmd = &cobra.Command{
	Use:   "faces",
	Short: "Detect and store face embeddings for all photos",
	Long: `Detect faces in all photos and store their embeddings in PostgreSQL.
Each face is stored with its embedding (512 dimensions), bounding box, and detection score.

The process can be stopped and resumed - already processed photos are skipped.

Examples:
  # Detect faces in all photos (5 concurrent workers)
  photo-sorter photo faces

  # Use different concurrency
  photo-sorter photo faces --concurrency 3

  # Limit number of photos to process
  photo-sorter photo faces --limit 100`,
	RunE: runPhotoFaces,
}

func init() {
	photoCmd.AddCommand(photoFacesCmd)

	photoFacesCmd.Flags().Int("concurrency", 5, "Number of parallel workers")
	photoFacesCmd.Flags().Int("limit", 0, "Limit number of photos to process (0 = no limit)")
}

func runPhotoFaces(cmd *cobra.Command, args []string) error {
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	limit, _ := cmd.Flags().GetInt("limit")

	ctx := context.Background()
	cfg := config.Load()

	// Connect to PhotoPrism
	fmt.Println("Connecting to PhotoPrism...")
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

	// Connect to PostgreSQL
	fmt.Println("Connecting to PostgreSQL...")
	pool, err := database.Connect(ctx, &cfg.Postgres)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	// Run migrations
	if err := database.Migrate(ctx, pool, cfg.Embedding.Dim); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	faceRepo := database.NewFaceRepository(pool)

	// Get current counts
	faceCount, _ := faceRepo.Count(ctx)
	photoCount, _ := faceRepo.CountPhotos(ctx)
	fmt.Printf("Faces in database: %d (across %d photos)\n", faceCount, photoCount)

	// Create embedding client
	embURL := cfg.Embedding.URL
	if embURL == "" {
		embURL = cfg.LlamaCpp.URL
	}
	embClient := fingerprint.NewEmbeddingClient(embURL, "faces")

	// Fetch all photos from PhotoPrism (paginated)
	fmt.Println("Fetching photos from PhotoPrism...")
	var allPhotos []photoprism.Photo
	pageSize := 1000
	offset := 0

	for {
		photos, err := pp.GetPhotos(pageSize, offset)
		if err != nil {
			return fmt.Errorf("failed to get photos: %w", err)
		}
		if len(photos) == 0 {
			break
		}
		allPhotos = append(allPhotos, photos...)
		offset += len(photos)

		// Apply limit if set
		if limit > 0 && len(allPhotos) >= limit {
			allPhotos = allPhotos[:limit]
			break
		}
	}

	fmt.Printf("Total photos in PhotoPrism: %d\n", len(allPhotos))

	// Filter out photos that already have faces processed
	var photosToProcess []photoprism.Photo
	for _, photo := range allPhotos {
		has, err := faceRepo.HasFaces(ctx, photo.UID)
		if err != nil {
			return fmt.Errorf("failed to check faces: %w", err)
		}
		if !has {
			photosToProcess = append(photosToProcess, photo)
		}
	}

	if len(photosToProcess) == 0 {
		fmt.Println("All photos already have faces processed!")
		return nil
	}

	fmt.Printf("Photos to process: %d (skipping %d already processed)\n\n",
		len(photosToProcess), len(allPhotos)-len(photosToProcess))

	// Create progress bar
	bar := progressbar.NewOptions(len(photosToProcess),
		progressbar.OptionSetDescription("Detecting faces"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)

	// Process photos with concurrency
	var successCount, errorCount, totalFaces int
	var mu sync.Mutex

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, photo := range photosToProcess {
		wg.Add(1)
		go func(p photoprism.Photo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Download original photo (no resize - bbox coordinates are relative to original)
			imageData, _, err := pp.GetPhotoDownload(p.UID)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			// Detect faces
			result, err := embClient.ComputeFaceEmbeddings(ctx, imageData)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			// Convert to StoredFace and save
			faces := make([]database.StoredFace, len(result.Faces))
			for i, f := range result.Faces {
				faces[i] = database.StoredFace{
					PhotoUID:  p.UID,
					FaceIndex: f.FaceIndex,
					Embedding: f.Embedding,
					BBox:      f.BBox,
					DetScore:  f.DetScore,
					Model:     result.Model,
					Dim:       f.Dim,
				}
			}

			// Save faces (even if empty, to mark photo as processed)
			if err := faceRepo.SaveFaces(ctx, p.UID, faces); err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			mu.Lock()
			successCount++
			totalFaces += len(faces)
			mu.Unlock()
			bar.Add(1)
		}(photo)
	}

	wg.Wait()
	fmt.Println()

	// Final stats
	finalFaceCount, _ := faceRepo.Count(ctx)
	finalPhotoCount, _ := faceRepo.CountPhotos(ctx)
	fmt.Printf("\nCompleted: %d photos processed, %d errors\n", successCount, errorCount)
	fmt.Printf("New faces detected: %d\n", totalFaces)
	fmt.Printf("Total faces in database: %d (across %d photos)\n", finalFaceCount, finalPhotoCount)

	return nil
}
