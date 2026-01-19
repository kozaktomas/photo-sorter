package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/tomas/photo-sorter/internal/config"
	"github.com/tomas/photo-sorter/internal/database"
	"github.com/tomas/photo-sorter/internal/fingerprint"
	"github.com/tomas/photo-sorter/internal/photoprism"
)

var photoInfoCmd = &cobra.Command{
	Use:   "info [photo-uid]",
	Short: "Display photo information including perceptual hashes and embeddings",
	Long: `Display detailed information about a photo including metadata, perceptual
hashes (pHash and dHash), and optionally image embeddings for similarity matching.

Examples:
  # Get info for a single photo
  photo-sorter photo info pq8abc123def

  # Get info for all photos in an album
  photo-sorter photo info --album aq8xyz789ghi

  # Include image embeddings (requires llama.cpp with Qwen2.5-VL model)
  photo-sorter photo info --embedding pq8abc123def

  # Output as JSON with embeddings
  photo-sorter photo info --json --embedding pq8abc123def

  # Process album with limited concurrency
  photo-sorter photo info --album aq8xyz789ghi --concurrency 3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPhotoInfo,
}

func init() {
	photoCmd.AddCommand(photoInfoCmd)

	photoInfoCmd.Flags().String("album", "", "Process all photos in an album")
	photoInfoCmd.Flags().Bool("json", false, "Output as JSON")
	photoInfoCmd.Flags().Int("limit", 0, "Limit number of photos (0 = no limit)")
	photoInfoCmd.Flags().Int("concurrency", 5, "Number of parallel workers")
	photoInfoCmd.Flags().Bool("embedding", false, "Compute image embeddings using llama.cpp (Qwen2.5-VL model)")
}

func runPhotoInfo(cmd *cobra.Command, args []string) error {
	albumUID, _ := cmd.Flags().GetString("album")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	limit, _ := cmd.Flags().GetInt("limit")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	computeEmbedding, _ := cmd.Flags().GetBool("embedding")

	// Validate args
	if albumUID == "" && len(args) == 0 {
		return fmt.Errorf("either provide a photo UID or use --album flag")
	}
	if albumUID != "" && len(args) > 0 {
		return fmt.Errorf("cannot specify both photo UID and --album flag")
	}

	ctx := context.Background()
	cfg := config.Load()

	// Connect to PhotoPrism
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

	// Create embedding client and repository if needed
	var embClient *fingerprint.EmbeddingClient
	var embRepo *database.EmbeddingRepository
	if computeEmbedding {
		// Use embedding URL from config, fall back to llama.cpp URL for backwards compatibility
		embURL := cfg.Embedding.URL
		if embURL == "" {
			embURL = cfg.LlamaCpp.URL
		}
		embClient = fingerprint.NewEmbeddingClient(embURL, "clip")

		// Connect to PostgreSQL
		pool, err := database.Connect(ctx, &cfg.Postgres)
		if err != nil {
			return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
		defer pool.Close()

		// Run migrations
		if err := database.Migrate(ctx, pool, cfg.Embedding.Dim); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		embRepo = database.NewEmbeddingRepository(pool)

		if !jsonOutput {
			count, _ := embRepo.Count(ctx)
			fmt.Printf("Embedding database: %d entries\n\n", count)
		}
	}

	if albumUID != "" {
		return runPhotoInfoAlbum(pp, albumUID, limit, concurrency, jsonOutput, embClient, embRepo)
	}
	return runPhotoInfoSingle(pp, args[0], jsonOutput, embClient, embRepo)
}

func runPhotoInfoSingle(pp *photoprism.PhotoPrism, photoUID string, jsonOutput bool, embClient *fingerprint.EmbeddingClient, embRepo *database.EmbeddingRepository) error {
	ctx := context.Background()

	// Get photo metadata
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return fmt.Errorf("failed to get photo details: %w", err)
	}

	// Download photo for hash computation
	imageData, _, err := pp.GetPhotoDownload(photoUID)
	if err != nil {
		return fmt.Errorf("failed to download photo: %w", err)
	}

	// Compute hashes
	hashes, err := fingerprint.ComputeHashes(imageData)
	if err != nil {
		return fmt.Errorf("failed to compute hashes: %w", err)
	}

	// Build PhotoInfo from details map
	info := buildPhotoInfo(details, hashes)

	// Compute embedding if client is provided
	if embClient != nil {
		// Check database first
		if embRepo != nil {
			if stored, err := embRepo.Get(ctx, photoUID); err == nil && stored != nil {
				info.Embedding = stored.Embedding
				info.EmbeddingTime = 0 // cached, no time
			}
		}

		// Compute if not in database
		if info.Embedding == nil {
			startTime := time.Now()
			result, err := embClient.ComputeEmbeddingWithMetadata(ctx, imageData)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to compute embedding: %v\n", err)
			} else {
				info.Embedding = result.Embedding
				info.EmbeddingTime = time.Since(startTime).Seconds()
				// Save to database
				if embRepo != nil {
					if err := embRepo.Save(ctx, photoUID, result.Embedding, result.Model, result.Pretrained, result.Dim); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to save embedding: %v\n", err)
					}
				}
			}
		}
	}

	if jsonOutput {
		return outputJSON(info)
	}

	return outputHumanReadableSingle(info)
}

func runPhotoInfoAlbum(pp *photoprism.PhotoPrism, albumUID string, limit, concurrency int, jsonOutput bool, embClient *fingerprint.EmbeddingClient, embRepo *database.EmbeddingRepository) error {
	ctx := context.Background()

	// Get album info
	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}

	// Fetch photos
	if limit == 0 {
		limit = 10000
	}
	photos, err := pp.GetAlbumPhotos(albumUID, limit, 0)
	if err != nil {
		return fmt.Errorf("failed to get album photos: %w", err)
	}

	if len(photos) == 0 {
		if jsonOutput {
			return outputJSON(fingerprint.PhotoInfoBatch{Photos: []fingerprint.PhotoInfo{}, Count: 0})
		}
		fmt.Println("No photos found in album.")
		return nil
	}

	if !jsonOutput {
		fmt.Printf("Album: %s\n", album.Title)
		fmt.Printf("Processing %d photos...\n\n", len(photos))
	}

	// Process photos with concurrency
	results := make([]fingerprint.PhotoInfo, len(photos))
	var errors []error
	var mu sync.Mutex

	// Create progress bar (only for non-JSON output)
	var bar *progressbar.ProgressBar
	if !jsonOutput {
		description := "Computing hashes"
		if embClient != nil {
			description = "Computing hashes + embeddings"
		}
		bar = progressbar.NewOptions(len(photos),
			progressbar.OptionSetDescription(description),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetItsString("photos"),
			progressbar.OptionShowElapsedTimeOnFinish(),
			progressbar.OptionSetPredictTime(true),
			progressbar.OptionFullWidth(),
		)
	}

	// Semaphore for concurrency control
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, photo := range photos {
		wg.Add(1)
		go func(idx int, p photoprism.Photo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Download photo
			imageData, _, err := pp.GetPhotoDownload(p.UID)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("photo %s: %w", p.UID, err))
				mu.Unlock()
				if bar != nil {
					bar.Add(1)
				}
				return
			}

			// Compute hashes
			hashes, err := fingerprint.ComputeHashes(imageData)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("photo %s: %w", p.UID, err))
				mu.Unlock()
				if bar != nil {
					bar.Add(1)
				}
				return
			}

			// Build info from Photo struct
			info := buildPhotoInfoFromPhoto(p, hashes)

			// Compute embedding if client is provided
			if embClient != nil {
				// Check database first
				if embRepo != nil {
					if stored, err := embRepo.Get(ctx, p.UID); err == nil && stored != nil {
						info.Embedding = stored.Embedding
						info.EmbeddingTime = 0 // cached
					}
				}

				// Compute if not in database
				if info.Embedding == nil {
					startTime := time.Now()
					result, err := embClient.ComputeEmbeddingWithMetadata(ctx, imageData)
					if err != nil {
						mu.Lock()
						errors = append(errors, fmt.Errorf("photo %s embedding: %v", p.UID, err))
						mu.Unlock()
					} else {
						info.Embedding = result.Embedding
						info.EmbeddingTime = time.Since(startTime).Seconds()
						// Save to database
						if embRepo != nil {
							if err := embRepo.Save(ctx, p.UID, result.Embedding, result.Model, result.Pretrained, result.Dim); err != nil {
								mu.Lock()
								errors = append(errors, fmt.Errorf("photo %s save: %v", p.UID, err))
								mu.Unlock()
							}
						}
					}
				}
			}

			mu.Lock()
			results[idx] = info
			mu.Unlock()

			if bar != nil {
				bar.Add(1)
			}
		}(i, photo)
	}

	wg.Wait()

	if bar != nil {
		fmt.Println()
	}

	// Filter out empty results (from errors)
	validResults := make([]fingerprint.PhotoInfo, 0, len(results))
	for _, r := range results {
		if r.UID != "" {
			validResults = append(validResults, r)
		}
	}

	if jsonOutput {
		batch := fingerprint.PhotoInfoBatch{
			Photos: validResults,
			Count:  len(validResults),
		}
		return outputJSON(batch)
	}

	// Output table
	outputHumanReadableBatch(validResults, embClient != nil)

	// Report errors
	if len(errors) > 0 {
		fmt.Printf("\nErrors: %d\n", len(errors))
		for _, e := range errors {
			fmt.Printf("  - %v\n", e)
		}
	}

	return nil
}

func buildPhotoInfo(details map[string]interface{}, hashes *fingerprint.HashResult) fingerprint.PhotoInfo {
	info := fingerprint.PhotoInfo{
		PHash:      hashes.PHash,
		DHash:      hashes.DHash,
		PHashBits:  hashes.PHashBits,
		DHashBits:  hashes.DHashBits,
		ComputedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Extract fields from details map
	if v, ok := details["UID"].(string); ok {
		info.UID = v
	}
	if v, ok := details["OriginalName"].(string); ok {
		info.OriginalName = v
	}
	if v, ok := details["FileName"].(string); ok {
		info.FileName = v
	}
	if v, ok := details["Width"].(float64); ok {
		info.Width = int(v)
	}
	if v, ok := details["Height"].(float64); ok {
		info.Height = int(v)
	}
	if v, ok := details["TakenAt"].(string); ok {
		info.TakenAt = v
	}
	if v, ok := details["Year"].(float64); ok {
		info.Year = int(v)
	}
	if v, ok := details["Month"].(float64); ok {
		info.Month = int(v)
	}
	if v, ok := details["Day"].(float64); ok {
		info.Day = int(v)
	}
	if v, ok := details["Lat"].(float64); ok {
		info.Lat = v
	}
	if v, ok := details["Lng"].(float64); ok {
		info.Lng = v
	}
	if v, ok := details["Country"].(string); ok {
		info.Country = v
	}
	if v, ok := details["CameraModel"].(string); ok {
		info.CameraModel = v
	}
	if v, ok := details["Hash"].(string); ok {
		info.Hash = v
	}
	if v, ok := details["Title"].(string); ok {
		info.Title = v
	}
	if v, ok := details["Description"].(string); ok {
		info.Description = v
	}

	return info
}

func buildPhotoInfoFromPhoto(p photoprism.Photo, hashes *fingerprint.HashResult) fingerprint.PhotoInfo {
	return fingerprint.PhotoInfo{
		UID:          p.UID,
		OriginalName: p.OriginalName,
		FileName:     p.FileName,
		Width:        p.Width,
		Height:       p.Height,
		TakenAt:      p.TakenAt,
		Year:         p.Year,
		Month:        p.Month,
		Day:          p.Day,
		Lat:          p.Lat,
		Lng:          p.Lng,
		Country:      p.Country,
		CameraModel:  p.CameraModel,
		Hash:         p.Hash,
		Title:        p.Title,
		Description:  p.Description,
		PHash:        hashes.PHash,
		DHash:        hashes.DHash,
		PHashBits:    hashes.PHashBits,
		DHashBits:    hashes.DHashBits,
		ComputedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

func outputJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func outputHumanReadableSingle(info fingerprint.PhotoInfo) error {
	fmt.Printf("Photo: %s\n", info.UID)
	fmt.Println("────────────────────────────────────────")

	fmt.Println("\nMetadata:")
	if info.OriginalName != "" {
		fmt.Printf("  Original Name:  %s\n", info.OriginalName)
	}
	if info.FileName != "" {
		fmt.Printf("  File Name:      %s\n", info.FileName)
	}
	if info.Width > 0 && info.Height > 0 {
		fmt.Printf("  Dimensions:     %d x %d\n", info.Width, info.Height)
	}
	if info.CameraModel != "" {
		fmt.Printf("  Camera:         %s\n", info.CameraModel)
	}

	if info.TakenAt != "" || info.Year > 0 {
		fmt.Println("\nDates:")
		if info.TakenAt != "" {
			fmt.Printf("  Taken At:       %s\n", info.TakenAt)
		}
		if info.Year > 0 {
			fmt.Printf("  Year/Month/Day: %d / %02d / %02d\n", info.Year, info.Month, info.Day)
		}
	}

	if info.Lat != 0 || info.Lng != 0 || info.Country != "" {
		fmt.Println("\nLocation:")
		if info.Lat != 0 || info.Lng != 0 {
			fmt.Printf("  GPS:            %.6f, %.6f\n", info.Lat, info.Lng)
		}
		if info.Country != "" && info.Country != "zz" {
			fmt.Printf("  Country:        %s\n", info.Country)
		}
	}

	fmt.Println("\nHashes:")
	fmt.Printf("  pHash:          %s\n", info.PHash)
	fmt.Printf("  dHash:          %s\n", info.DHash)
	if info.Hash != "" {
		fmt.Printf("  PhotoPrism:     %s\n", info.Hash)
	}

	if len(info.Embedding) > 0 {
		fmt.Println("\nEmbedding:")
		fmt.Printf("  Dimensions:     %d\n", len(info.Embedding))
		fmt.Printf("  Model:          Qwen2.5-VL-7B-Instruct\n")
		if info.EmbeddingTime > 0 {
			fmt.Printf("  Time:           %.2fs\n", info.EmbeddingTime)
		} else {
			fmt.Printf("  Time:           (cached)\n")
		}
	}

	return nil
}

func outputHumanReadableBatch(results []fingerprint.PhotoInfo, hasEmbedding bool) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if hasEmbedding {
		fmt.Fprintln(w, "UID\tDIMENSIONS\tTAKEN\tPHASH\tDHASH\tEMBEDDING")
		fmt.Fprintln(w, "---\t----------\t-----\t-----\t-----\t---------")
	} else {
		fmt.Fprintln(w, "UID\tDIMENSIONS\tTAKEN\tPHASH\tDHASH")
		fmt.Fprintln(w, "---\t----------\t-----\t-----\t-----")
	}

	for _, info := range results {
		taken := ""
		if info.Year > 0 {
			taken = fmt.Sprintf("%d-%02d-%02d", info.Year, info.Month, info.Day)
		}
		dims := ""
		if info.Width > 0 && info.Height > 0 {
			dims = fmt.Sprintf("%dx%d", info.Width, info.Height)
		}
		if hasEmbedding {
			embDim := "-"
			if len(info.Embedding) > 0 {
				embDim = fmt.Sprintf("%d dims", len(info.Embedding))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				info.UID, dims, taken, info.PHash, info.DHash, embDim)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				info.UID, dims, taken, info.PHash, info.DHash)
		}
	}

	w.Flush()
	fmt.Printf("\nTotal: %d photos\n", len(results))
}
