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

var photoEmbedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Compute embeddings for all photos",
	Long: `Compute and store image embeddings for all photos in PhotoPrism.
Embeddings are stored in PostgreSQL with pgvector for efficient similarity search.

The process can be stopped and resumed - already processed photos are skipped.

Examples:
  # Compute embeddings for all photos (5 concurrent workers)
  photo-sorter photo embed

  # Use different concurrency
  photo-sorter photo embed --concurrency 3

  # Limit number of photos to process
  photo-sorter photo embed --limit 100`,
	RunE: runPhotoEmbed,
}

func init() {
	photoCmd.AddCommand(photoEmbedCmd)

	photoEmbedCmd.Flags().Int("concurrency", 5, "Number of parallel workers")
	photoEmbedCmd.Flags().Int("limit", 0, "Limit number of photos to process (0 = no limit)")
}

func runPhotoEmbed(cmd *cobra.Command, args []string) error {
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

	embRepo := database.NewEmbeddingRepository(pool)

	// Get current count
	dbCount, _ := embRepo.Count(ctx)
	fmt.Printf("Embeddings in database: %d\n", dbCount)

	// Create embedding client
	embURL := cfg.Embedding.URL
	if embURL == "" {
		embURL = cfg.LlamaCpp.URL
	}
	embClient := fingerprint.NewEmbeddingClient(embURL, "clip")

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

	// Filter out photos that already have embeddings
	var photosToProcess []photoprism.Photo
	for _, photo := range allPhotos {
		has, err := embRepo.Has(ctx, photo.UID)
		if err != nil {
			return fmt.Errorf("failed to check embedding: %w", err)
		}
		if !has {
			photosToProcess = append(photosToProcess, photo)
		}
	}

	if len(photosToProcess) == 0 {
		fmt.Println("All photos already have embeddings!")
		return nil
	}

	fmt.Printf("Photos to process: %d (skipping %d already processed)\n\n",
		len(photosToProcess), len(allPhotos)-len(photosToProcess))

	// Create progress bar
	bar := progressbar.NewOptions(len(photosToProcess),
		progressbar.OptionSetDescription("Computing embeddings"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)

	// Process photos with concurrency
	var successCount, errorCount int
	var mu sync.Mutex

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, photo := range photosToProcess {
		wg.Add(1)
		go func(p photoprism.Photo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Download original photo
			imageData, _, err := pp.GetPhotoDownload(p.UID)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			// Resize to max 1920px
			resizedData, err := fingerprint.ResizeImage(imageData, 1920)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			// Compute embedding
			result, err := embClient.ComputeEmbeddingWithMetadata(ctx, resizedData)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			// Save to database
			if err := embRepo.Save(ctx, p.UID, result.Embedding, result.Model, result.Pretrained, result.Dim); err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				bar.Add(1)
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
			bar.Add(1)
		}(photo)
	}

	wg.Wait()
	fmt.Println()

	// Final stats
	finalCount, _ := embRepo.Count(ctx)
	fmt.Printf("\nCompleted: %d successful, %d errors\n", successCount, errorCount)
	fmt.Printf("Total embeddings in database: %d\n", finalCount)

	return nil
}
