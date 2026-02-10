package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mariadb"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
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
	cachePushEmbeddingsCmd.Flags().Bool("recompute-centroids", false, "Recompute face cluster centroids from new embeddings")
	cachePushEmbeddingsCmd.Flags().Bool("json", false, "Output as JSON")
}

// PushEmbeddingsResult represents the result of a push-embeddings operation
type PushEmbeddingsResult struct {
	Success             bool   `json:"success"`
	TotalFaces          int    `json:"total_faces"`
	MarkersUpdated      int    `json:"markers_updated"`
	MarkerErrors        int    `json:"marker_errors"`
	ClustersUpdated     int    `json:"clusters_updated"`
	ClusterErrors       int    `json:"cluster_errors"`
	DryRun              bool   `json:"dry_run"`
	DurationMs          int64  `json:"duration_ms"`
	DurationHuman       string `json:"duration_human,omitempty"`
}

func runCachePushEmbeddings(cmd *cobra.Command, args []string) error {
	dryRun := mustGetBool(cmd, "dry-run")
	recomputeCentroids := mustGetBool(cmd, "recompute-centroids")
	jsonOutput := mustGetBool(cmd, "json")

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	// Validate PhotoPrism database URL
	if cfg.PhotoPrism.DatabaseURL == "" {
		return errors.New("PHOTOPRISM_DATABASE_URL environment variable is required")
	}

	// Initialize PostgreSQL
	if cfg.Database.URL == "" {
		return errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	faceRepo := postgres.NewFaceRepository(pool)
	database.RegisterPostgresBackend(
		nil,
		func() database.FaceReader { return faceRepo },
		nil,
	)

	// Connect to MariaDB
	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism MariaDB...")
	}
	mariaPool, err := mariadb.NewPool(cfg.PhotoPrism.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to MariaDB: %w", err)
	}
	defer mariaPool.Close()

	// Get faces with marker linkage from PostgreSQL
	if !jsonOutput {
		fmt.Println("Fetching faces with marker linkage from PostgreSQL...")
	}
	faces, err := faceRepo.GetFacesWithMarkerUID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get faces: %w", err)
	}

	if len(faces) == 0 {
		result := PushEmbeddingsResult{
			Success:    true,
			TotalFaces: 0,
			DryRun:     dryRun,
			DurationMs: time.Since(startTime).Milliseconds(),
		}
		if jsonOutput {
			return outputJSON(result)
		}
		fmt.Println("No faces with marker linkage found.")
		return nil
	}

	if !jsonOutput {
		fmt.Printf("Found %d faces with marker linkage\n", len(faces))
		if dryRun {
			fmt.Println("DRY RUN - no changes will be written")
		}
		fmt.Println()
	}

	// Push embeddings to MariaDB markers
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

	// Recompute centroids if requested
	clustersUpdated := 0
	clusterErrors := 0

	if recomputeCentroids {
		if !jsonOutput {
			fmt.Printf("\nRecomputing face cluster centroids...\n")
		}

		clusters, err := mariaPool.GetFaceClusters(ctx)
		if err != nil {
			return fmt.Errorf("failed to get face clusters: %w", err)
		}

		// Build marker_uid → embedding lookup from our faces
		embeddingByMarker := make(map[string][]float32, len(faces))
		for i := range faces {
			embeddingByMarker[faces[i].MarkerUID] = faces[i].Embedding
		}

		for _, cluster := range clusters {
			// Collect embeddings for this cluster's markers
			var clusterEmbeddings [][]float32
			for _, markerUID := range cluster.MarkerUIDs {
				if emb, ok := embeddingByMarker[markerUID]; ok {
					clusterEmbeddings = append(clusterEmbeddings, emb)
				}
			}

			if len(clusterEmbeddings) == 0 {
				if !jsonOutput {
					fmt.Printf("  Cluster %s: no InsightFace embeddings found (%d markers), skipping\n", cluster.ID, len(cluster.MarkerUIDs))
				}
				continue
			}

			// Compute centroid (element-wise mean)
			dim := len(clusterEmbeddings[0])
			centroid := make([]float32, dim)
			for _, emb := range clusterEmbeddings {
				for i, v := range emb {
					centroid[i] += v
				}
			}
			n := float32(len(clusterEmbeddings))
			for i := range centroid {
				centroid[i] /= n
			}

			if !jsonOutput {
				fmt.Printf("  Cluster %s: %d/%d markers have embeddings", cluster.ID, len(clusterEmbeddings), len(cluster.MarkerUIDs))
			}

			if dryRun {
				if !jsonOutput {
					fmt.Printf(" → would update centroid\n")
				}
				clustersUpdated++
				continue
			}

			if err := mariaPool.UpdateFaceClusterEmbedding(ctx, cluster.ID, centroid); err != nil {
				if !jsonOutput {
					fmt.Printf(" → ERROR: %v\n", err)
				}
				clusterErrors++
				continue
			}

			if !jsonOutput {
				fmt.Printf(" → updated centroid\n")
			}
			clustersUpdated++
		}
	}

	duration := time.Since(startTime)
	result := PushEmbeddingsResult{
		Success:         true,
		TotalFaces:      len(faces),
		MarkersUpdated:  markersUpdated,
		MarkerErrors:    markerErrors,
		ClustersUpdated: clustersUpdated,
		ClusterErrors:   clusterErrors,
		DryRun:          dryRun,
		DurationMs:      duration.Milliseconds(),
		DurationHuman:   formatDuration(duration),
	}

	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}

	// Human-readable summary
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

	return nil
}
