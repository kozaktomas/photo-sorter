package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mariadb"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/spf13/cobra"
)

var cachePushEmbeddingsCmd = &cobra.Command{
	Use:   "push-embeddings",
	Short: "Push InsightFace embeddings to PhotoPrism MariaDB",
	Long: `Push InsightFace (buffalo_l/ResNet100) face embeddings from the local PostgreSQL
cache to PhotoPrism's MariaDB database, replacing the default TensorFlow embeddings.

This updates markers.embeddings_json for all faces that have a marker_uid linkage.
Optionally recomputes face cluster centroids from the new embeddings.

Requires PHOTOPRISM_DATABASE_URL to be set (MariaDB DSN).

Examples:
  # Preview changes
  photo-sorter cache push-embeddings --dry-run

  # Push embeddings
  photo-sorter cache push-embeddings

  # Push and recompute centroids
  photo-sorter cache push-embeddings --recompute-centroids

  # JSON output
  photo-sorter cache push-embeddings --json`,
	RunE: runCachePushEmbeddings,
}

func init() {
	cacheCmd.AddCommand(cachePushEmbeddingsCmd)

	cachePushEmbeddingsCmd.Flags().Bool("dry-run", false, "Preview changes without writing to MariaDB")
	cachePushEmbeddingsCmd.Flags().Bool("recompute-centroids", false, "Recompute face cluster centroids")
	cachePushEmbeddingsCmd.Flags().Bool("json", false, "Output as JSON")
}

// PushEmbeddingsResult represents the result of a push-embeddings operation.
type PushEmbeddingsResult struct {
	Success         bool   `json:"success"`
	TotalFaces      int    `json:"total_faces"`
	MarkersUpdated  int    `json:"markers_updated"`
	MarkerErrors    int    `json:"marker_errors"`
	ClustersUpdated int    `json:"clusters_updated"`
	ClusterErrors   int    `json:"cluster_errors"`
	DryRun          bool   `json:"dry_run"`
	DurationMs      int64  `json:"duration_ms"`
	DurationHuman   string `json:"duration_human,omitempty"`
}

// pushMarkerEmbeddings pushes face embeddings to MariaDB markers.
// Returns (markersUpdated, markerErrors).
func pushMarkerEmbeddings(
	ctx context.Context, mariaPool *mariadb.Pool, faces []database.StoredFace,
	dryRun bool, jsonOutput bool,
) (int, int) {
	markersUpdated := 0
	markerErrors := 0

	for i := range faces {
		face := &faces[i]
		if !jsonOutput {
			fmt.Printf("  Marker %s (photo %s, face %d)", face.MarkerUID, face.PhotoUID, face.FaceIndex)
		}

		if dryRun {
			if !jsonOutput {
				fmt.Printf(" → would update (%d-dim embedding)\n", len(face.Embedding))
			}
			markersUpdated++
			continue
		}

		if err := mariaPool.UpdateMarkerEmbedding(ctx, face.MarkerUID, face.Embedding); err != nil {
			if !jsonOutput {
				fmt.Printf(" → ERROR: %v\n", err)
			}
			markerErrors++
			continue
		}

		if !jsonOutput {
			fmt.Printf(" → updated\n")
		}
		markersUpdated++
	}
	return markersUpdated, markerErrors
}

// computeClusterCentroid computes the element-wise mean of embeddings.
func computeClusterCentroid(embeddings [][]float32) []float32 {
	dim := len(embeddings[0])
	centroid := make([]float32, dim)
	for _, emb := range embeddings {
		for i, v := range emb {
			centroid[i] += v
		}
	}
	n := float32(len(embeddings))
	for i := range centroid {
		centroid[i] /= n
	}
	return centroid
}

// collectClusterEmbeddings collects InsightFace embeddings for a cluster's markers.
func collectClusterEmbeddings(cluster mariadb.FaceCluster, embeddingByMarker map[string][]float32) [][]float32 {
	var clusterEmbeddings [][]float32
	for _, markerUID := range cluster.MarkerUIDs {
		if emb, ok := embeddingByMarker[markerUID]; ok {
			clusterEmbeddings = append(clusterEmbeddings, emb)
		}
	}
	return clusterEmbeddings
}

// updateClusterCentroid updates or previews a single cluster centroid.
// Returns (updated bool, error bool).
func updateClusterCentroid(
	ctx context.Context, mariaPool *mariadb.Pool, cluster mariadb.FaceCluster,
	centroid []float32, dryRun bool, jsonOutput bool,
) (bool, bool) {
	if !jsonOutput {
		fmt.Printf("  Cluster %s: embeddings available", cluster.ID)
	}

	if dryRun {
		if !jsonOutput {
			fmt.Printf(" → would update centroid\n")
		}
		return true, false
	}

	if err := mariaPool.UpdateFaceClusterEmbedding(ctx, cluster.ID, centroid); err != nil {
		if !jsonOutput {
			fmt.Printf(" → ERROR: %v\n", err)
		}
		return false, true
	}

	if !jsonOutput {
		fmt.Printf(" → updated centroid\n")
	}
	return true, false
}

// buildEmbeddingByMarkerMap creates a map from marker UID to embedding for quick lookup.
func buildEmbeddingByMarkerMap(faces []database.StoredFace) map[string][]float32 {
	embeddingByMarker := make(map[string][]float32, len(faces))
	for i := range faces {
		embeddingByMarker[faces[i].MarkerUID] = faces[i].Embedding
	}
	return embeddingByMarker
}

// recomputeFaceCentroids recomputes face cluster centroids from new embeddings.
// Returns (clustersUpdated, clusterErrors, error).
func recomputeFaceCentroids(
	ctx context.Context, mariaPool *mariadb.Pool, faces []database.StoredFace,
	dryRun bool, jsonOutput bool,
) (int, int, error) {
	if !jsonOutput {
		fmt.Printf("\nRecomputing face cluster centroids...\n")
	}

	clusters, err := mariaPool.GetFaceClusters(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get face clusters: %w", err)
	}

	embeddingByMarker := buildEmbeddingByMarkerMap(faces)
	clustersUpdated := 0
	clusterErrors := 0

	for _, cluster := range clusters {
		clusterEmbeddings := collectClusterEmbeddings(cluster, embeddingByMarker)
		if len(clusterEmbeddings) == 0 {
			if !jsonOutput {
				fmt.Printf("  Cluster %s: no InsightFace embeddings found (%d markers), skipping\n",
					cluster.ID, len(cluster.MarkerUIDs))
			}
			continue
		}

		centroid := computeClusterCentroid(clusterEmbeddings)
		updated, errored := updateClusterCentroid(ctx, mariaPool, cluster, centroid, dryRun, jsonOutput)
		if updated {
			clustersUpdated++
		}
		if errored {
			clusterErrors++
		}
	}

	return clustersUpdated, clusterErrors, nil
}

// printPushEmbeddingsResult prints the human-readable summary.
func printPushEmbeddingsResult(result PushEmbeddingsResult, recomputeCentroids bool, dryRun bool) {
	fmt.Println("\nPush complete!")
	fmt.Printf("  Faces found:       %d\n", result.TotalFaces)
	fmt.Printf("  Markers updated:   %d\n", result.MarkersUpdated)
	if result.MarkerErrors > 0 {
		fmt.Printf("  Marker errors:     %d\n", result.MarkerErrors)
	}
	if recomputeCentroids {
		fmt.Printf("  Clusters updated:  %d\n", result.ClustersUpdated)
		if result.ClusterErrors > 0 {
			fmt.Printf("  Cluster errors:    %d\n", result.ClusterErrors)
		}
	}
	if dryRun {
		fmt.Printf("  Mode:              DRY RUN\n")
	}
	fmt.Printf("  Duration:          %s\n", result.DurationHuman)
}

// pushEmbeddingsDeps holds dependencies for the push-embeddings command.
type pushEmbeddingsDeps struct {
	mariaPool *mariadb.Pool
	faceRepo  *postgres.FaceRepository
}

// initPushEmbeddingsDeps initializes PostgreSQL and MariaDB dependencies.
func initPushEmbeddingsDeps(cfg *config.Config, jsonOutput bool) (*pushEmbeddingsDeps, error) {
	if cfg.PhotoPrism.DatabaseURL == "" {
		return nil, errors.New("PHOTOPRISM_DATABASE_URL environment variable is required")
	}
	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(nil, func() database.FaceReader { return faceRepo }, nil)

	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism MariaDB...")
	}
	mariaPool, err := mariadb.NewPool(cfg.PhotoPrism.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MariaDB: %w", err)
	}

	return &pushEmbeddingsDeps{mariaPool: mariaPool, faceRepo: faceRepo}, nil
}

// outputPushEmbeddingsResult outputs the result as JSON or human-readable.
func outputPushEmbeddingsResult(
	result PushEmbeddingsResult, recomputeCentroids bool, dryRun bool, jsonOutput bool,
) error {
	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}
	printPushEmbeddingsResult(result, recomputeCentroids, dryRun)
	return nil
}

func runCachePushEmbeddings(cmd *cobra.Command, args []string) error {
	dryRun := mustGetBool(cmd, "dry-run")
	recomputeCentroidsFlag := mustGetBool(cmd, "recompute-centroids")
	jsonOutput := mustGetBool(cmd, "json")

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	deps, err := initPushEmbeddingsDeps(cfg, jsonOutput)
	if err != nil {
		return err
	}
	defer deps.mariaPool.Close()

	if !jsonOutput {
		fmt.Println("Fetching faces with marker linkage from PostgreSQL...")
	}
	faces, err := deps.faceRepo.GetFacesWithMarkerUID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get faces: %w", err)
	}

	if len(faces) == 0 {
		return outputPushEmbeddingsResult(PushEmbeddingsResult{
			Success: true, TotalFaces: 0, DryRun: dryRun, DurationMs: time.Since(startTime).Milliseconds(),
		}, recomputeCentroidsFlag, dryRun, jsonOutput)
	}

	if !jsonOutput {
		fmt.Printf("Found %d faces with marker linkage\n", len(faces))
		if dryRun {
			fmt.Println("DRY RUN - no changes will be written")
		}
		fmt.Println()
	}

	markersUpdated, markerErrors := pushMarkerEmbeddings(ctx, deps.mariaPool, faces, dryRun, jsonOutput)

	clustersUpdated := 0
	clusterErrors := 0
	if recomputeCentroidsFlag {
		clustersUpdated, clusterErrors, err = recomputeFaceCentroids(ctx, deps.mariaPool, faces, dryRun, jsonOutput)
		if err != nil {
			return err
		}
	}

	duration := time.Since(startTime)
	return outputPushEmbeddingsResult(PushEmbeddingsResult{
		Success: true, TotalFaces: len(faces), MarkersUpdated: markersUpdated, MarkerErrors: markerErrors,
		ClustersUpdated: clustersUpdated, ClusterErrors: clusterErrors, DryRun: dryRun,
		DurationMs: duration.Milliseconds(), DurationHuman: formatDuration(duration),
	}, recomputeCentroidsFlag, dryRun, jsonOutput)
}
