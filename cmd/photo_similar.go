package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tomas/photo-sorter/internal/config"
	"github.com/tomas/photo-sorter/internal/database"
)

var photoSimilarCmd = &cobra.Command{
	Use:   "similar <photo-uid>",
	Short: "Find similar photos based on image embeddings",
	Long: `Find photos similar to a given photo using image embeddings stored in PostgreSQL.

This command uses cosine distance on image embeddings to find visually similar photos.
Lower distance values indicate more similar images.

Prerequisites:
- Run 'photo info --embedding' or 'photo embed' to compute embeddings first

Examples:
  # Find similar photos with default threshold (0.3)
  photo-sorter photo similar pq8abc123def

  # Use stricter threshold (lower = more similar)
  photo-sorter photo similar pq8abc123def --threshold 0.2

  # Limit results
  photo-sorter photo similar pq8abc123def --limit 10

  # Output as JSON
  photo-sorter photo similar pq8abc123def --json`,
	Args: cobra.ExactArgs(1),
	RunE: runPhotoSimilar,
}

func init() {
	photoCmd.AddCommand(photoSimilarCmd)

	photoSimilarCmd.Flags().Float64("threshold", 0.3, "Maximum cosine distance for similarity (lower = more similar)")
	photoSimilarCmd.Flags().Int("limit", 50, "Maximum number of results")
	photoSimilarCmd.Flags().Bool("json", false, "Output as JSON")
}

// SimilarPhoto represents a similar photo result
type SimilarPhoto struct {
	PhotoUID   string  `json:"photo_uid"`
	Distance   float64 `json:"distance"`
	Similarity float64 `json:"similarity"` // 1 - distance, for easier interpretation
}

// SimilarOutput represents the JSON output structure
type SimilarOutput struct {
	SourcePhotoUID string         `json:"source_photo_uid"`
	Threshold      float64        `json:"threshold"`
	Results        []SimilarPhoto `json:"results"`
	Count          int            `json:"count"`
}

func runPhotoSimilar(cmd *cobra.Command, args []string) error {
	photoUID := args[0]
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	limit, _ := cmd.Flags().GetInt("limit")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	ctx := context.Background()
	cfg := config.Load()

	// Connect to PostgreSQL
	if !jsonOutput {
		fmt.Println("Connecting to PostgreSQL...")
	}
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
		fmt.Printf("No similar photos found for %s within threshold %.2f\n", photoUID, threshold)
		return nil
	}

	fmt.Printf("Found %d similar photos:\n\n", len(results))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PHOTO UID\tDISTANCE\tSIMILARITY")
	fmt.Fprintln(w, "---------\t--------\t----------")

	for _, r := range results {
		fmt.Fprintf(w, "%s\t%.4f\t%.2f%%\n", r.PhotoUID, r.Distance, r.Similarity*100)
	}

	w.Flush()

	return nil
}
