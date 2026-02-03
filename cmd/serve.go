package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/web"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web server",
	Long: `Start the Photo Sorter web server.
The web server provides a browser-based interface for managing albums,
sorting photos with AI, and viewing results.`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().Int("port", 8080, "Port to listen on")
	serveCmd.Flags().String("host", "0.0.0.0", "Host to bind to")
	serveCmd.Flags().String("session-secret", "", "Secret for signing session cookies (defaults to random)")
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg := config.Load()

	// Initialize PostgreSQL database backend (required)
	if cfg.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}

	fmt.Printf("Connecting to PostgreSQL database...\n")
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	// Create singleton repositories
	pool := postgres.GetGlobalPool()
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	faceRepo := postgres.NewFaceRepository(pool)

	ctx := context.Background()

	// Build or load in-memory HNSW index for fast face similarity search
	faceIndexPath := cfg.Database.HNSWIndexPath
	if faceIndexPath != "" {
		fmt.Printf("Loading face HNSW index from %s...\n", faceIndexPath)
	} else {
		fmt.Printf("Building in-memory HNSW index for face matching...\n")
	}
	if err := faceRepo.EnableHNSW(ctx, faceIndexPath); err != nil {
		fmt.Printf("Warning: Failed to build face HNSW index: %v\n", err)
		fmt.Printf("Face matching will use PostgreSQL queries (slower)\n")
	} else {
		if faceIndexPath != "" {
			fmt.Printf("Face HNSW index ready with %d faces (persisted to %s)\n", faceRepo.HNSWCount(), faceIndexPath)
		} else {
			fmt.Printf("Face HNSW index built with %d faces (in-memory only)\n", faceRepo.HNSWCount())
		}
	}

	// Build or load in-memory HNSW index for fast image embedding similarity search
	embIndexPath := cfg.Database.HNSWEmbeddingIndexPath
	if embIndexPath != "" {
		fmt.Printf("Loading embedding HNSW index from %s...\n", embIndexPath)
	} else {
		fmt.Printf("Building in-memory HNSW index for image embeddings...\n")
	}
	if err := embeddingRepo.EnableHNSW(ctx, embIndexPath); err != nil {
		fmt.Printf("Warning: Failed to build embedding HNSW index: %v\n", err)
		fmt.Printf("Expand/Similar will use PostgreSQL queries (slower)\n")
	} else {
		if embIndexPath != "" {
			fmt.Printf("Embedding HNSW index ready with %d embeddings (persisted to %s)\n", embeddingRepo.HNSWCount(), embIndexPath)
		} else {
			fmt.Printf("Embedding HNSW index built with %d embeddings (in-memory only)\n", embeddingRepo.HNSWCount())
		}
	}

	// Register PostgreSQL as the active backend using singletons
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)
	// Register embedding writer for sync cache cleanup
	database.RegisterEmbeddingWriter(func() database.EmbeddingWriter { return embeddingRepo })
	// Register HNSW rebuilders for the Rebuild Index feature
	database.RegisterFaceHNSWRebuilder(faceRepo)
	database.RegisterEmbeddingHNSWRebuilder(embeddingRepo)
	fmt.Printf("Using PostgreSQL backend\n")

	// Create session repository for persistent sessions
	sessionRepo := postgres.NewSessionRepository(pool)
	fmt.Printf("Session persistence enabled (PostgreSQL)\n")

	port := mustGetInt(cmd, "port")
	host := mustGetString(cmd, "host")
	sessionSecret := mustGetString(cmd, "session-secret")

	// Use environment variable if flag not set
	if sessionSecret == "" {
		sessionSecret = os.Getenv("WEB_SESSION_SECRET")
	}

	// Override with environment variables if set
	if envPort := os.Getenv("WEB_PORT"); envPort != "" {
		fmt.Sscanf(envPort, "%d", &port)
	}
	if envHost := os.Getenv("WEB_HOST"); envHost != "" {
		host = envHost
	}

	// Validate PhotoPrism connection
	if cfg.PhotoPrism.URL == "" {
		return fmt.Errorf("PHOTOPRISM_URL environment variable is required")
	}

	// Create and start server
	server := web.NewServer(cfg, port, host, sessionSecret, sessionRepo)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")

		// Save face HNSW index before shutdown (if PostgreSQL backend with persistence)
		if rebuilder := database.GetFaceHNSWRebuilder(); rebuilder != nil {
			if err := rebuilder.SaveHNSWIndex(); err != nil {
				fmt.Printf("Warning: failed to save face HNSW index: %v\n", err)
			} else {
				fmt.Println("Face HNSW index saved to disk")
			}
		}

		// Save embedding HNSW index before shutdown (if PostgreSQL backend with persistence)
		if rebuilder := database.GetEmbeddingHNSWRebuilder(); rebuilder != nil {
			if err := rebuilder.SaveHNSWIndex(); err != nil {
				fmt.Printf("Warning: failed to save embedding HNSW index: %v\n", err)
			} else {
				fmt.Println("Embedding HNSW index saved to disk")
			}
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
	}()

	fmt.Printf("Starting Photo Sorter Web UI on http://%s:%d\n", host, port)
	fmt.Println("Press Ctrl+C to stop")

	if err := server.Start(); err != nil {
		return err
	}

	return nil
}
