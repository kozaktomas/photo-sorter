package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/web"
	"github.com/spf13/cobra"
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

// initFaceHNSW builds or loads the face HNSW index for fast similarity search.
func initFaceHNSW(ctx context.Context, faceRepo *postgres.FaceRepository, indexPath string) {
	if indexPath != "" {
		fmt.Printf("Loading face HNSW index from %s...\n", indexPath)
	} else {
		fmt.Printf("Building in-memory HNSW index for face matching...\n")
	}
	if err := faceRepo.EnableHNSW(ctx, indexPath); err != nil {
		fmt.Printf("Warning: Failed to build face HNSW index: %v\n", err)
		fmt.Printf("Face matching will use PostgreSQL queries (slower)\n")
	} else if indexPath != "" {
		fmt.Printf("Face HNSW index ready with %d faces (persisted to %s)\n", faceRepo.HNSWCount(), indexPath)
	} else {
		fmt.Printf("Face HNSW index built with %d faces (in-memory only)\n", faceRepo.HNSWCount())
	}
}

// initEmbeddingHNSW builds or loads the embedding HNSW index for fast similarity search.
func initEmbeddingHNSW(ctx context.Context, embeddingRepo *postgres.EmbeddingRepository, indexPath string) {
	if indexPath != "" {
		fmt.Printf("Loading embedding HNSW index from %s...\n", indexPath)
	} else {
		fmt.Printf("Building in-memory HNSW index for image embeddings...\n")
	}
	if err := embeddingRepo.EnableHNSW(ctx, indexPath); err != nil {
		fmt.Printf("Warning: Failed to build embedding HNSW index: %v\n", err)
		fmt.Printf("Expand/Similar will use PostgreSQL queries (slower)\n")
	} else if indexPath != "" {
		fmt.Printf("Embedding HNSW index ready with %d embeddings (persisted to %s)\n", embeddingRepo.HNSWCount(), indexPath)
	} else {
		fmt.Printf("Embedding HNSW index built with %d embeddings (in-memory only)\n", embeddingRepo.HNSWCount())
	}
}

// registerServeBackends registers all database backends and repositories for the serve command.
func registerServeBackends(
	pool *postgres.Pool, embeddingRepo *postgres.EmbeddingRepository, faceRepo *postgres.FaceRepository,
) *postgres.SessionRepository {
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)
	database.RegisterEmbeddingWriter(func() database.EmbeddingWriter { return embeddingRepo })
	eraRepo := postgres.NewEraEmbeddingRepository(pool)
	database.RegisterEraEmbeddingWriter(func() database.EraEmbeddingWriter { return eraRepo })
	database.RegisterFaceHNSWRebuilder(faceRepo)
	database.RegisterEmbeddingHNSWRebuilder(embeddingRepo)
	fmt.Printf("Using PostgreSQL backend\n")

	bookRepo := postgres.NewBookRepository(pool)
	database.RegisterBookWriter(func() database.BookWriter { return bookRepo })
	fmt.Printf("Photo book storage enabled (PostgreSQL)\n")

	sessionRepo := postgres.NewSessionRepository(pool)
	fmt.Printf("Session persistence enabled (PostgreSQL)\n")
	return sessionRepo
}

// resolveServeHostPort resolves port and host from flags and environment variables.
func resolveServeHostPort(cmd *cobra.Command) (int, string, string) {
	port := mustGetInt(cmd, "port")
	host := mustGetString(cmd, "host")
	sessionSecret := mustGetString(cmd, "session-secret")

	if sessionSecret == "" {
		sessionSecret = os.Getenv("WEB_SESSION_SECRET")
	}
	if envPort := os.Getenv("WEB_PORT"); envPort != "" {
		fmt.Sscanf(envPort, "%d", &port)
	}
	if envHost := os.Getenv("WEB_HOST"); envHost != "" {
		host = envHost
	}
	return port, host, sessionSecret
}

// saveHNSWIndexes saves all HNSW indexes to disk during shutdown.
func saveHNSWIndexes() {
	if rebuilder := database.GetFaceHNSWRebuilder(); rebuilder != nil {
		if err := rebuilder.SaveHNSWIndex(); err != nil {
			fmt.Printf("Warning: failed to save face HNSW index: %v\n", err)
		} else {
			fmt.Println("Face HNSW index saved to disk")
		}
	}
	if rebuilder := database.GetEmbeddingHNSWRebuilder(); rebuilder != nil {
		if err := rebuilder.SaveHNSWIndex(); err != nil {
			fmt.Printf("Warning: failed to save embedding HNSW index: %v\n", err)
		} else {
			fmt.Println("Embedding HNSW index saved to disk")
		}
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg := config.Load()

	if cfg.Database.URL == "" {
		return errors.New("DATABASE_URL environment variable is required")
	}

	fmt.Printf("Connecting to PostgreSQL database...\n")
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	faceRepo := postgres.NewFaceRepository(pool)
	ctx := context.Background()

	initFaceHNSW(ctx, faceRepo, cfg.Database.HNSWIndexPath)
	initEmbeddingHNSW(ctx, embeddingRepo, cfg.Database.HNSWEmbeddingIndexPath)

	sessionRepo := registerServeBackends(pool, embeddingRepo, faceRepo)
	port, host, sessionSecret := resolveServeHostPort(cmd)

	if cfg.PhotoPrism.URL == "" {
		return errors.New("PHOTOPRISM_URL environment variable is required")
	}

	server := web.NewServer(cfg, port, host, sessionSecret, sessionRepo)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		saveHNSWIndexes()

		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
	}()

	fmt.Printf("Starting Photo Sorter Web UI on http://%s:%d\n", host, port)
	fmt.Println("Press Ctrl+C to stop")

	if err := server.Start(); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}
	return nil
}
