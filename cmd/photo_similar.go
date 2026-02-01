package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var photoSimilarCmd = &cobra.Command{
	Use:   "similar [photo-uid]",
	Short: "Find similar photos based on image embeddings",
	Long: `Find photos similar to a given photo or photos with specific labels.

This command uses cosine distance on image embeddings to find visually similar photos.
Lower distance values indicate more similar images.

Prerequisites:
- Run 'photo info --embedding' or 'photo embed' to compute embeddings first

Examples:
  # Find similar photos to a single photo
  photo-sorter photo similar pq8abc123def

  # Find similar photos based on label (finds photos similar to all photos with that label)
  photo-sorter photo similar --label "cat"

  # Multiple labels (finds photos similar to photos with any of these labels)
  photo-sorter photo similar --label "cat" --label "dog"

  # Use stricter threshold (lower = more similar)
  photo-sorter photo similar --label "cat" --threshold 0.2

  # Limit results
  photo-sorter photo similar --label "cat" --limit 10

  # Output as JSON
  photo-sorter photo similar --label "cat" --json

  # Preview label assignments (dry-run)
  photo-sorter photo similar --label "cat" --apply --dry-run

  # Apply labels to similar photos
  photo-sorter photo similar --label "cat" --apply`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPhotoSimilar,
}

func init() {
	photoCmd.AddCommand(photoSimilarCmd)

	photoSimilarCmd.Flags().Float64("threshold", 0.3, "Maximum cosine distance for similarity (lower = more similar)")
	photoSimilarCmd.Flags().Int("limit", 50, "Maximum number of results")
	photoSimilarCmd.Flags().Bool("json", false, "Output as JSON")
	photoSimilarCmd.Flags().StringSlice("label", nil, "Find photos similar to all photos with this label (can be specified multiple times)")
	photoSimilarCmd.Flags().Bool("apply", false, "Apply the label(s) to similar photos found")
	photoSimilarCmd.Flags().Bool("dry-run", false, "Preview label assignments without applying them")
}

// SimilarPhoto represents a similar photo result
type SimilarPhoto struct {
	PhotoUID   string  `json:"photo_uid"`
	Distance   float64 `json:"distance"`
	Similarity float64 `json:"similarity"`  // 1 - distance, for easier interpretation
	Applied    bool    `json:"applied,omitempty"`
	ApplyError string  `json:"apply_error,omitempty"`
}

// LabelSimilarOutput represents the JSON output structure for label-based search
type LabelSimilarOutput struct {
	Labels       []string       `json:"labels"`
	SourcePhotos []string       `json:"source_photos"`
	Threshold    float64        `json:"threshold"`
	Results      []SimilarPhoto `json:"results"`
	Count        int            `json:"count"`
	Applied      int            `json:"applied,omitempty"`
	Failed       int            `json:"failed,omitempty"`
}

// SimilarOutput represents the JSON output structure for single photo search
type SimilarOutput struct {
	SourcePhotoUID string         `json:"source_photo_uid"`
	Threshold      float64        `json:"threshold"`
	Results        []SimilarPhoto `json:"results"`
	Count          int            `json:"count"`
}

func runPhotoSimilar(cmd *cobra.Command, args []string) error {
	threshold := mustGetFloat64(cmd, "threshold")
	limit := mustGetInt(cmd, "limit")
	jsonOutput := mustGetBool(cmd, "json")
	labels := mustGetStringSlice(cmd, "label")
	apply := mustGetBool(cmd, "apply")
	dryRun := mustGetBool(cmd, "dry-run")

	// Determine mode: label-based or single photo
	if len(labels) > 0 {
		return runPhotoSimilarByLabel(labels, threshold, limit, jsonOutput, apply, dryRun)
	}

	// --apply only works with --label
	if apply || dryRun {
		return fmt.Errorf("--apply and --dry-run flags require --label flag")
	}

	// Single photo mode - require exactly one argument
	if len(args) != 1 {
		return fmt.Errorf("requires either a photo-uid argument or --label flag")
	}

	return runPhotoSimilarByUID(args[0], threshold, limit, jsonOutput)
}

func runPhotoSimilarByLabel(labels []string, threshold float64, limit int, jsonOutput bool, apply bool, dryRun bool) error {
	ctx := context.Background()
	cfg := config.Load()

	// Initialize PostgreSQL database
	if cfg.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	// Create singleton repositories and register with database package
	pool := postgres.GetGlobalPool()
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	// Connect to PhotoPrism
	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism...")
	}
	pp, err := photoprism.NewPhotoPrism(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}

	// Get embedding reader from PostgreSQL
	embRepo, err := database.GetEmbeddingReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get embedding reader: %w", err)
	}
	if !jsonOutput {
		fmt.Println("Using PostgreSQL data source")
	}

	// Collect all source photos from all labels
	sourcePhotoUIDs := make(map[string]bool)
	for _, label := range labels {
		if !jsonOutput {
			fmt.Printf("Fetching photos with label '%s'...\n", label)
		}

		query := fmt.Sprintf("label:%s", label)
		offset := 0
		pageSize := 100

		for {
			photos, err := pp.GetPhotosWithQuery(pageSize, offset, query)
			if err != nil {
				return fmt.Errorf("failed to get photos for label '%s': %w", label, err)
			}

			for _, photo := range photos {
				sourcePhotoUIDs[photo.UID] = true
			}

			if len(photos) < pageSize {
				break
			}
			offset += pageSize
		}
	}

	if len(sourcePhotoUIDs) == 0 {
		if !jsonOutput {
			fmt.Printf("No photos found with labels: %v\n", labels)
		}
		return nil
	}

	if !jsonOutput {
		fmt.Printf("Found %d source photos with specified labels\n", len(sourcePhotoUIDs))
	}

	// Get embeddings for source photos and find similar
	// Track how many source embeddings match each candidate (same logic as photo match)
	type matchCandidate struct {
		PhotoUID   string
		Distance   float64 // Best (lowest) distance
		MatchCount int     // Number of source embeddings that matched
	}
	candidateMap := make(map[string]*matchCandidate)
	sourceList := make([]string, 0, len(sourcePhotoUIDs))
	sourceEmbeddingCount := 0

	for photoUID := range sourcePhotoUIDs {
		sourceList = append(sourceList, photoUID)

		emb, err := embRepo.Get(ctx, photoUID)
		if err != nil {
			return fmt.Errorf("failed to get embedding for %s: %w", photoUID, err)
		}
		if emb == nil {
			if !jsonOutput {
				fmt.Printf("Warning: no embedding for source photo %s (skipping)\n", photoUID)
			}
			continue
		}
		sourceEmbeddingCount++

		// Find similar photos for this source
		similar, distances, err := embRepo.FindSimilarWithDistance(ctx, emb.Embedding, limit*10, threshold)
		if err != nil {
			return fmt.Errorf("failed to find similar photos: %w", err)
		}

		for i, sim := range similar {
			// Skip source photos
			if sourcePhotoUIDs[sim.PhotoUID] {
				continue
			}

			// Track match count and keep best (lowest) distance
			if existing, ok := candidateMap[sim.PhotoUID]; ok {
				existing.MatchCount++
				if distances[i] < existing.Distance {
					existing.Distance = distances[i]
				}
			} else {
				candidateMap[sim.PhotoUID] = &matchCandidate{
					PhotoUID:   sim.PhotoUID,
					Distance:   distances[i],
					MatchCount: 1,
				}
			}
		}
	}

	// Calculate minimum match count: at least 5% (rounded up), minimum 5
	minMatchCount := (sourceEmbeddingCount + 19) / 20
	if minMatchCount < 5 {
		minMatchCount = 5
	}

	if !jsonOutput {
		fmt.Printf("Requiring at least %d/%d source matches\n", minMatchCount, sourceEmbeddingCount)
	}

	// Filter: only keep candidates that matched at least minMatchCount sources
	for photoUID, candidate := range candidateMap {
		if candidate.MatchCount < minMatchCount {
			delete(candidateMap, photoUID)
		}
	}

	// Convert to sorted results
	var results []SimilarPhoto
	for _, candidate := range candidateMap {
		results = append(results, SimilarPhoto{
			PhotoUID:   candidate.PhotoUID,
			Distance:   candidate.Distance,
			Similarity: 1 - candidate.Distance,
		})
	}

	// Sort by distance (ascending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Distance < results[j].Distance
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	// Human-readable table output (before apply, unless JSON)
	if !jsonOutput {
		if len(results) == 0 {
			fmt.Printf("No similar photos found within threshold %.2f\n", threshold)
			return nil
		}

		fmt.Printf("\nFound %d similar photos (not already labeled):\n\n", len(results))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PHOTO\tDISTANCE\tSIMILARITY")
		fmt.Fprintln(w, "-----\t--------\t----------")

		for _, r := range results {
			photoRef := r.PhotoUID
			if url := cfg.PhotoPrism.PhotoURL(r.PhotoUID); url != "" {
				photoRef = url
			}
			fmt.Fprintf(w, "%s\t%.4f\t%.2f%%\n", photoRef, r.Distance, r.Similarity*100)
		}

		w.Flush()
	}

	// Apply labels if requested
	appliedCount := 0
	failedCount := 0

	if apply && len(results) > 0 {
		if !jsonOutput {
			if dryRun {
				fmt.Printf("\n[DRY-RUN] Would apply %d label(s) to %d photos:\n", len(labels), len(results))
				for _, label := range labels {
					fmt.Printf("  Label: %q\n", label)
				}
			} else {
				fmt.Printf("\nApplying %d label(s) to %d photos...\n", len(labels), len(results))
			}
		}

		for i := range results {
			r := &results[i]

			if dryRun {
				if !jsonOutput {
					fmt.Printf("  [DRY-RUN] %s\n", r.PhotoUID)
				}
				continue
			}

			// Apply all labels to this photo
			photoApplied := true
			var lastError string
			for _, label := range labels {
				photoLabel := photoprism.PhotoLabel{
					Name:     label,
					LabelSrc: "manual",
				}
				_, err := pp.AddPhotoLabel(r.PhotoUID, photoLabel)
				if err != nil {
					photoApplied = false
					lastError = err.Error()
					if !jsonOutput {
						fmt.Printf("  Error: failed to add label %q to %s: %v\n", label, r.PhotoUID, err)
					}
				}
			}

			if photoApplied {
				r.Applied = true
				appliedCount++
				if !jsonOutput {
					fmt.Printf("  Applied labels to %s\n", r.PhotoUID)
				}
			} else {
				r.ApplyError = lastError
				failedCount++
			}
		}

		if !jsonOutput && !dryRun {
			fmt.Printf("\nApplied: %d, Failed: %d\n", appliedCount, failedCount)
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(LabelSimilarOutput{
			Labels:       labels,
			SourcePhotos: sourceList,
			Threshold:    threshold,
			Results:      results,
			Count:        len(results),
			Applied:      appliedCount,
			Failed:       failedCount,
		})
	}

	return nil
}

func runPhotoSimilarByUID(photoUID string, threshold float64, limit int, jsonOutput bool) error {
	ctx := context.Background()
	cfg := config.Load()

	// Initialize PostgreSQL database
	if cfg.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	// Create singleton repositories and register with database package
	pool := postgres.GetGlobalPool()
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	// Get embedding reader from PostgreSQL
	embRepo, err := database.GetEmbeddingReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get embedding reader: %w", err)
	}
	if !jsonOutput {
		fmt.Println("Using PostgreSQL data source")
	}

	// Get the source photo's embedding
	if !jsonOutput {
		fmt.Printf("Looking up embedding for %s...\n", photoUID)
	}

	sourceEmb, err := embRepo.Get(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("failed to get embedding: %w", err)
	}
	if sourceEmb == nil {
		return fmt.Errorf("no embedding found for photo %s. Run 'photo info --embedding %s' first", photoUID, photoUID)
	}

	// Search for similar photos (+1 to account for the source photo itself)
	if !jsonOutput {
		fmt.Printf("Searching for similar photos (threshold: %.2f)...\n\n", threshold)
	}

	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, sourceEmb.Embedding, limit+1, threshold)
	if err != nil {
		return fmt.Errorf("failed to find similar photos: %w", err)
	}

	// Build results, excluding the source photo
	var results []SimilarPhoto
	for i, emb := range similar {
		if emb.PhotoUID == photoUID {
			continue // Skip the source photo
		}
		results = append(results, SimilarPhoto{
			PhotoUID:   emb.PhotoUID,
			Distance:   distances[i],
			Similarity: 1 - distances[i],
		})
	}

	// Apply limit after filtering
	if len(results) > limit {
		results = results[:limit]
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(SimilarOutput{
			SourcePhotoUID: photoUID,
			Threshold:      threshold,
			Results:        results,
			Count:          len(results),
		})
	}

	// Human-readable output
	if len(results) == 0 {
		sourceRef := photoUID
		if url := cfg.PhotoPrism.PhotoURL(photoUID); url != "" {
			sourceRef = url
		}
		fmt.Printf("No similar photos found for %s within threshold %.2f\n", sourceRef, threshold)
		return nil
	}

	fmt.Printf("Found %d similar photos:\n\n", len(results))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PHOTO\tDISTANCE\tSIMILARITY")
	fmt.Fprintln(w, "-----\t--------\t----------")

	for _, r := range results {
		photoRef := r.PhotoUID
		if url := cfg.PhotoPrism.PhotoURL(r.PhotoUID); url != "" {
			photoRef = url
		}
		fmt.Fprintf(w, "%s\t%.4f\t%.2f%%\n", photoRef, r.Distance, r.Similarity*100)
	}

	w.Flush()

	return nil
}
