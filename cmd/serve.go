package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	mcpserver "github.com/kozaktomas/photo-sorter/internal/mcp"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/trash"
	"github.com/kozaktomas/photo-sorter/internal/web"
	"github.com/kozaktomas/photo-sorter/internal/web/handlers"
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

	photoRepo := postgres.NewPhotoRepository(pool)
	database.RegisterPhotoWriter(func() database.PhotoWriter { return photoRepo })

	albumRepo := postgres.NewAlbumRepository(pool)
	database.RegisterAlbumWriter(func() database.AlbumWriter { return albumRepo })

	labelRepo := postgres.NewLabelRepository(pool)
	database.RegisterLabelWriter(func() database.LabelWriter { return labelRepo })

	subjectRepo := postgres.NewSubjectRepository(pool)
	database.RegisterSubjectWriter(func() database.SubjectWriter { return subjectRepo })

	markerRepo := postgres.NewMarkerRepository(pool)
	database.RegisterMarkerWriter(func() database.MarkerWriter { return markerRepo })

	phashRepo := postgres.NewPHashRepository(pool)
	database.RegisterPHashWriter(func() database.PHashWriter { return phashRepo })
	fmt.Printf("Native photo + album + label + subject + marker + phash storage enabled (PostgreSQL)\n")

	userRepo := postgres.NewUserRepository(pool)
	database.RegisterUserWriter(func() database.UserWriter { return userRepo })
	fmt.Printf("Native user storage enabled (PostgreSQL)\n")

	tvRepo := postgres.NewTextVersionRepository(pool)
	database.RegisterTextVersionStore(func() database.TextVersionStore { return tvRepo })

	tcRepo := postgres.NewTextCheckRepository(pool)
	database.RegisterTextCheckStore(func() database.TextCheckStore { return tcRepo })

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

// initMCPHandler initialises the MCP HTTP handler when MCP_API_TOKEN is set
// and returns nil otherwise. The returned handler is plumbed into the web
// server, where a nil value disables the /mcp/* routes.
func initMCPHandler(ctx context.Context, cfg *config.Config) (http.Handler, error) {
	apiToken := os.Getenv("MCP_API_TOKEN")
	if apiToken == "" {
		fmt.Println("MCP_API_TOKEN not set, MCP endpoint disabled")
		return nil, nil //nolint:nilnil // nil handler signals "MCP disabled" to caller
	}
	h, err := buildMCPHandler(ctx, cfg, apiToken)
	if err != nil {
		return nil, err
	}
	fmt.Println("MCP endpoint enabled at /mcp/sse")
	return h, nil
}

// bootstrapAdminUser auto-creates the initial admin user when the users
// table is empty and the BOOTSTRAP_ADMIN_* env vars are set. It is a no-op
// otherwise (subsequent restarts; or no env vars on a fresh install — a
// WARN is logged in that case). Must be called AFTER migrations run and
// AFTER the user repository is registered.
func bootstrapAdminUser(ctx context.Context, cfg *config.Config) error {
	userReader, err := database.GetUserReader(ctx)
	if err != nil {
		return fmt.Errorf("user reader: %w", err)
	}
	userWriter, err := database.GetUserWriter(ctx)
	if err != nil {
		return fmt.Errorf("user writer: %w", err)
	}
	if err := auth.BootstrapAdmin(ctx, userReader, userWriter, *cfg); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	return nil
}

// resolveTrashStore wires the dependencies that internal/trash.PurgePhoto
// needs to hard-delete a photo. Must be called after registerServeBackends.
// Returns nil and logs a warning when any dependency is missing — the
// auto-purge daemon then refuses to start rather than partially purging.
func resolveTrashStore(ctx context.Context, cfg *config.Config) *trash.Store {
	photoWriter, err := database.GetPhotoWriter(ctx)
	if err != nil {
		fmt.Printf("trash: photo writer unavailable: %v (auto-purge disabled)\n", err)
		return nil
	}
	embWriter, err := database.GetEmbeddingWriter(ctx)
	if err != nil {
		fmt.Printf("trash: embedding writer unavailable: %v (auto-purge disabled)\n", err)
		return nil
	}
	faceWriter, err := database.GetFaceWriter(ctx)
	if err != nil {
		fmt.Printf("trash: face writer unavailable: %v (auto-purge disabled)\n", err)
		return nil
	}
	store, err := storage.New(cfg.Storage.OriginalsPath, cfg.Storage.CachePath)
	if err != nil {
		fmt.Printf("trash: on-disk storage unavailable: %v (auto-purge disabled)\n", err)
		return nil
	}
	return &trash.Store{
		Photos:     photoWriter,
		Embeddings: embWriter,
		Faces:      faceWriter,
		Files:      store,
	}
}

// trashRetention reads TRASH_RETENTION_DAYS and returns the configured
// retention window. Falls back to trash.DefaultRetention (30 days) when
// the env var is unset, empty, or unparseable so an operator typo can
// never accidentally turn the daemon into an aggressive eviction policy.
func trashRetention() time.Duration {
	raw := os.Getenv("TRASH_RETENTION_DAYS")
	if raw == "" {
		return trash.DefaultRetention
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days <= 0 {
		fmt.Printf("trash: invalid TRASH_RETENTION_DAYS=%q, using default\n", raw)
		return trash.DefaultRetention
	}
	return time.Duration(days) * 24 * time.Hour
}

// startTrashDaemon kicks off the auto-purge background goroutine. Returns
// the cancel function so gracefulShutdown can stop the daemon cleanly.
// No-op (returns a no-op cancel) when the trash store could not be
// resolved — the BatchPurge HTTP route already surfaces a 503 in that
// case, so the daemon just stays dark to match.
func startTrashDaemon(cfg *config.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	store := resolveTrashStore(ctx, cfg)
	if store == nil {
		cancel()
		return func() {}
	}
	retention := trashRetention()
	go trash.RunDaemon(ctx, trash.DefaultPurgeInterval, retention, store)
	return cancel
}

// gracefulShutdown waits for a signal, then shuts down the HTTP server, saves HNSW indexes, and closes the DB pool.
func gracefulShutdown(sigChan <-chan os.Signal, server *web.Server, pool *postgres.Pool) {
	<-sigChan
	fmt.Println("\nShutting down...")

	// 1. Stop accepting new requests first.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("Error during HTTP shutdown: %v\n", err)
	}

	// 2. Save HNSW indexes (no concurrent request modifications now).
	saveHNSWIndexes()

	// 3. Close the database pool last.
	if err := pool.Close(); err != nil {
		fmt.Printf("Error closing database pool: %v\n", err)
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

	if err := bootstrapAdminUser(ctx, cfg); err != nil {
		return err
	}

	port, host, sessionSecret := resolveServeHostPort(cmd)

	if cfg.PhotoPrism.URL == "" {
		return errors.New("PHOTOPRISM_URL environment variable is required")
	}

	handlers.SetVersionInfo(Version, CommitSHA)

	mcpHandler, err := initMCPHandler(ctx, cfg)
	if err != nil {
		return err
	}

	server := web.NewServer(cfg, port, host, sessionSecret, sessionRepo, mcpHandler)

	// Auto-purge daemon for trash retention. Started after the photo +
	// embedding + face writers are registered. The returned cancel is
	// invoked from gracefulShutdown so the goroutine drains cleanly.
	stopTrashDaemon := startTrashDaemon(cfg)
	defer stopTrashDaemon()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup
	wg.Go(func() { gracefulShutdown(sigChan, server, pool) })

	fmt.Printf("Starting Photo Sorter Web UI on http://%s:%d\n", host, port)
	fmt.Println("Press Ctrl+C to stop")

	if err := server.Start(); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	// Wait for shutdown goroutine to finish saving indexes and closing DB.
	wg.Wait()
	return nil
}

// buildMCPHandler creates an authenticated MCP HTTP handler.
func buildMCPHandler(ctx context.Context, cfg *config.Config, apiToken string) (http.Handler, error) {
	pp, err := photoprism.NewPhotoPrism(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword())
	if err != nil {
		return nil, fmt.Errorf("failed to create PhotoPrism session for MCP: %w", err)
	}

	bookWriter, err := database.GetBookWriter(ctx)
	if err != nil {
		return nil, fmt.Errorf("MCP: %w", err)
	}
	tvStore, err := database.GetTextVersionStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("MCP: %w", err)
	}
	tcStore, err := database.GetTextCheckStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("MCP: %w", err)
	}
	embReader, err := database.GetEmbeddingReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("MCP: %w", err)
	}

	mcpSrv := mcpserver.NewServer(
		Version, bookWriter, tvStore, tcStore, embReader,
		pp, cfg, apiToken, "/mcp",
	)
	return mcpserver.BearerAuthMiddleware(apiToken)(mcpSrv.Handler()), nil
}
