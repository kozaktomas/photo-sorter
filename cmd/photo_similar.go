package cmd

import (
	"context"
	"encoding/json"
	"errors"
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
		return errors.New("--apply and --dry-run flags require --label flag")
	}

	// Single photo mode - require exactly one argument
	if len(args) != 1 {
		return errors.New("requires either a photo-uid argument or --label flag")
	}

	return runPhotoSimilarByUID(args[0], threshold, limit, jsonOutput)
}

// similarLabelDeps holds initialized dependencies for label-based similar search.
type similarLabelDeps struct {
	pp      *photoprism.PhotoPrism
	embRepo database.EmbeddingReader
	cfg     *config.Config
}

// initSimilarLabelDeps initializes dependencies for label-based similar search.
func initSimilarLabelDeps(ctx context.Context, jsonOutput bool) (*similarLabelDeps, error) {
	cfg := config.Load()

	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism...")
	}
	pp, err := photoprism.NewPhotoPrism(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}

	embRepo, err := database.GetEmbeddingReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding reader: %w", err)
	}
	if !jsonOutput {
		fmt.Println("Using PostgreSQL data source")
	}

	return &similarLabelDeps{pp: pp, embRepo: embRepo, cfg: cfg}, nil
}

// fetchPhotosForLabels fetches all photo UIDs for the given labels.
func fetchPhotosForLabels(pp *photoprism.PhotoPrism, labels []string, jsonOutput bool) (map[string]bool, error) {
	sourcePhotoUIDs := make(map[string]bool)
	for _, label := range labels {
		if !jsonOutput {
			fmt.Printf("Fetching photos with label '%s'...\n", label)
		}

		query := "label:" + label
		offset := 0
		pageSize := 100

		for {
			photos, err := pp.GetPhotosWithQuery(pageSize, offset, query)
			if err != nil {
				return nil, fmt.Errorf("failed to get photos for label '%s': %w", label, err)
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
	return sourcePhotoUIDs, nil
}

// similarMatchCandidate tracks a candidate match for similar search.
type similarMatchCandidate struct {
	PhotoUID   string
	Distance   float64
	MatchCount int
}

// findSimilarByEmbeddings searches for similar photos using source embeddings.
func findSimilarByEmbeddings(ctx context.Context, embRepo database.EmbeddingReader, sourcePhotoUIDs map[string]bool, limit int, threshold float64, jsonOutput bool) (map[string]*similarMatchCandidate, []string, int, error) {
	candidateMap := make(map[string]*similarMatchCandidate)
	sourceList := make([]string, 0, len(sourcePhotoUIDs))
	sourceEmbeddingCount := 0

	for photoUID := range sourcePhotoUIDs {
		sourceList = append(sourceList, photoUID)

		emb, err := embRepo.Get(ctx, photoUID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to get embedding for %s: %w", photoUID, err)
		}
		if emb == nil {
			if !jsonOutput {
				fmt.Printf("Warning: no embedding for source photo %s (skipping)\n", photoUID)
			}
			continue
		}
		sourceEmbeddingCount++

		similar, distances, err := embRepo.FindSimilarWithDistance(ctx, emb.Embedding, limit*10, threshold)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to find similar photos: %w", err)
		}

		for i, sim := range similar {
			if sourcePhotoUIDs[sim.PhotoUID] {
				continue
			}
			if existing, ok := candidateMap[sim.PhotoUID]; ok {
				existing.MatchCount++
				if distances[i] < existing.Distance {
					existing.Distance = distances[i]
				}
			} else {
				candidateMap[sim.PhotoUID] = &similarMatchCandidate{
					PhotoUID: sim.PhotoUID, Distance: distances[i], MatchCount: 1,
				}
			}
		}
	}

	return candidateMap, sourceList, sourceEmbeddingCount, nil
}

// filterAndSortSimilarResults filters candidates by min match count and sorts by distance.
func filterAndSortSimilarResults(candidateMap map[string]*similarMatchCandidate, minMatchCount int, limit int) []SimilarPhoto {
	for photoUID, candidate := range candidateMap {
		if candidate.MatchCount < minMatchCount {
			delete(candidateMap, photoUID)
		}
	}

	var results []SimilarPhoto
	for _, candidate := range candidateMap {
		results = append(results, SimilarPhoto{
			PhotoUID: candidate.PhotoUID, Distance: candidate.Distance, Similarity: 1 - candidate.Distance,
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Distance < results[j].Distance })
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// printSimilarTable prints similar results as a human-readable table.
func printSimilarTable(results []SimilarPhoto, cfg *config.Config) {
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

// applyLabelsToPhoto applies all labels to a single photo. Returns true if all succeeded.
func applyLabelsToPhoto(pp *photoprism.PhotoPrism, r *SimilarPhoto, labels []string, jsonOutput bool) bool {
	allOK := true
	var lastError string
	for _, label := range labels {
		photoLabel := photoprism.PhotoLabel{Name: label, LabelSrc: "manual"}
		_, err := pp.AddPhotoLabel(r.PhotoUID, photoLabel)
		if err != nil {
			allOK = false
			lastError = err.Error()
			if !jsonOutput {
				fmt.Printf("  Error: failed to add label %q to %s: %v\n", label, r.PhotoUID, err)
			}
		}
	}
	if allOK {
		r.Applied = true
	} else {
		r.ApplyError = lastError
	}
	return allOK
}

// applySimilarLabels applies labels to similar photos.
func applySimilarLabels(pp *photoprism.PhotoPrism, results []SimilarPhoto, labels []string, dryRun bool, jsonOutput bool) (int, int) {
	if !jsonOutput {
		if dryRun {
			fmt.Printf("\n[DRY-RUN] Would apply %d label(s) to %d photos:\n", len(labels), len(results))
		} else {
			fmt.Printf("\nApplying %d label(s) to %d photos...\n", len(labels), len(results))
		}
	}

	appliedCount := 0
	failedCount := 0

	for i := range results {
		r := &results[i]
		if dryRun {
			if !jsonOutput {
				fmt.Printf("  [DRY-RUN] %s\n", r.PhotoUID)
			}
			continue
		}
		if applyLabelsToPhoto(pp, r, labels, jsonOutput) {
			appliedCount++
			if !jsonOutput {
				fmt.Printf("  Applied labels to %s\n", r.PhotoUID)
			}
		} else {
			failedCount++
		}
	}

	if !jsonOutput && !dryRun {
		fmt.Printf("\nApplied: %d, Failed: %d\n", appliedCount, failedCount)
	}

	return appliedCount, failedCount
}

// outputSimilarByLabelResults handles the output for label-based similar search (applying labels, printing, or JSON).
func outputSimilarByLabelResults(deps *similarLabelDeps, results []SimilarPhoto, labels []string, sourceList []string, threshold float64, apply bool, dryRun bool, jsonOutput bool) error {
	if !jsonOutput && len(results) == 0 {
		fmt.Printf("No similar photos found within threshold %.2f\n", threshold)
		return nil
	}

	if !jsonOutput {
		fmt.Printf("\nFound %d similar photos (not already labeled):\n\n", len(results))
		printSimilarTable(results, deps.cfg)
	}

	appliedCount := 0
	failedCount := 0
	if apply && len(results) > 0 {
		appliedCount, failedCount = applySimilarLabels(deps.pp, results, labels, dryRun, jsonOutput)
	}

	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(LabelSimilarOutput{
			Labels: labels, SourcePhotos: sourceList, Threshold: threshold,
			Results: results, Count: len(results), Applied: appliedCount, Failed: failedCount,
		}); err != nil {
			return fmt.Errorf("encoding JSON output: %w", err)
		}
		return nil
	}
	return nil
}

func runPhotoSimilarByLabel(labels []string, threshold float64, limit int, jsonOutput bool, apply bool, dryRun bool) error {
	ctx := context.Background()

	deps, err := initSimilarLabelDeps(ctx, jsonOutput)
	if err != nil {
		return err
	}

	sourcePhotoUIDs, err := fetchPhotosForLabels(deps.pp, labels, jsonOutput)
	if err != nil {
		return err
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

	candidateMap, sourceList, sourceEmbeddingCount, err := findSimilarByEmbeddings(ctx, deps.embRepo, sourcePhotoUIDs, limit, threshold, jsonOutput)
	if err != nil {
		return err
	}

	minMatchCount := max((sourceEmbeddingCount+19)/20, 5)
	if !jsonOutput {
		fmt.Printf("Requiring at least %d/%d source matches\n", minMatchCount, sourceEmbeddingCount)
	}

	results := filterAndSortSimilarResults(candidateMap, minMatchCount, limit)
	return outputSimilarByLabelResults(deps, results, labels, sourceList, threshold, apply, dryRun, jsonOutput)
}

// initSimilarUIDDeps initializes dependencies for single-photo similar search.
func initSimilarUIDDeps(ctx context.Context, jsonOutput bool) (database.EmbeddingReader, *config.Config, error) {
	cfg := config.Load()

	if cfg.Database.URL == "" {
		return nil, nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	embRepo, err := database.GetEmbeddingReader(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get embedding reader: %w", err)
	}
	if !jsonOutput {
		fmt.Println("Using PostgreSQL data source")
	}

	return embRepo, cfg, nil
}

// searchAndFilterSimilarByUID searches for similar photos and filters out the source photo.
func searchAndFilterSimilarByUID(ctx context.Context, embRepo database.EmbeddingReader, sourceEmb []float32, photoUID string, limit int, threshold float64) ([]SimilarPhoto, error) {
	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, sourceEmb, limit+1, threshold)
	if err != nil {
		return nil, fmt.Errorf("failed to find similar photos: %w", err)
	}

	var results []SimilarPhoto
	for i, emb := range similar {
		if emb.PhotoUID == photoUID {
			continue
		}
		results = append(results, SimilarPhoto{
			PhotoUID: emb.PhotoUID, Distance: distances[i], Similarity: 1 - distances[i],
		})
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func runPhotoSimilarByUID(photoUID string, threshold float64, limit int, jsonOutput bool) error {
	ctx := context.Background()

	embRepo, cfg, err := initSimilarUIDDeps(ctx, jsonOutput)
	if err != nil {
		return err
	}

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

	if !jsonOutput {
		fmt.Printf("Searching for similar photos (threshold: %.2f)...\n\n", threshold)
	}

	results, err := searchAndFilterSimilarByUID(ctx, embRepo, sourceEmb.Embedding, photoUID, limit, threshold)
	if err != nil {
		return err
	}

	return outputSimilarByUIDResults(results, photoUID, threshold, cfg, jsonOutput)
}

func outputSimilarByUIDResults(results []SimilarPhoto, photoUID string, threshold float64, cfg *config.Config, jsonOutput bool) error {
	if jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(SimilarOutput{
			SourcePhotoUID: photoUID, Threshold: threshold, Results: results, Count: len(results),
		}); err != nil {
			return fmt.Errorf("encoding JSON output: %w", err)
		}
		return nil
	}

	if len(results) == 0 {
		sourceRef := photoUID
		if url := cfg.PhotoPrism.PhotoURL(photoUID); url != "" {
			sourceRef = url
		}
		fmt.Printf("No similar photos found for %s within threshold %.2f\n", sourceRef, threshold)
		return nil
	}

	fmt.Printf("Found %d similar photos:\n\n", len(results))
	printSimilarTable(results, cfg)
	return nil
}
