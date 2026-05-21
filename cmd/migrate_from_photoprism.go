package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/migrate"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/spf13/cobra"
)

var migrateFromPhotoPrismCmd = &cobra.Command{
	Use:   "migrate-from-photoprism",
	Short: "One-shot import of an existing PhotoPrism instance",
	Long: `Import a PhotoPrism MariaDB database and on-disk originals into
photo-sorter's native PostgreSQL schema and storage tree.

The command is idempotent: photos already present in the destination
(identified by SHA256 file_hash) are skipped, and subjects, labels, and
albums are looked up by name/slug before being created. Re-running picks
up where the previous run stopped.

Examples:
  # Dry run — print counts without copying files or writing to Postgres.
  photo-sorter migrate-from-photoprism \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals \
      --pp-cache /photoprism/storage/cache \
      --uploader-username admin \
      --dry-run

  # Real migration without regenerating thumbnails (run cache compute-*
  # afterwards instead).
  photo-sorter migrate-from-photoprism \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals \
      --uploader-username admin \
      --skip-thumbs

  # Re-run only the markers stage (the photos must already exist).
  photo-sorter migrate-from-photoprism \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals \
      --uploader-username admin \
      --only markers`,
	RunE: runMigrateFromPhotoPrism,
}

func init() {
	rootCmd.AddCommand(migrateFromPhotoPrismCmd)

	migrateFromPhotoPrismCmd.Flags().String(
		"pp-db", "",
		"PhotoPrism MariaDB DSN, e.g. user:pw@tcp(host:3306)/photoprism (required)")
	migrateFromPhotoPrismCmd.Flags().String(
		"pp-originals", "",
		"Path to the PhotoPrism originals directory (required)")
	migrateFromPhotoPrismCmd.Flags().String(
		"pp-cache", "",
		"Path to the PhotoPrism storage/cache directory (optional)")
	migrateFromPhotoPrismCmd.Flags().String(
		"uploader-username", "",
		"Native photo-sorter username to attribute uploads to")
	migrateFromPhotoPrismCmd.Flags().Bool(
		"dry-run", false,
		"Walk PhotoPrism and print counts without writing to disk or Postgres")
	migrateFromPhotoPrismCmd.Flags().Bool(
		"skip-thumbs", false,
		"Skip thumbnail generation (run cache compute-* afterwards instead)")
	migrateFromPhotoPrismCmd.Flags().Int(
		"batch-size", 200,
		"Batch size for source DB queries")
	migrateFromPhotoPrismCmd.Flags().Int(
		"concurrency", 4,
		"Thumbnail generation worker count")
	migrateFromPhotoPrismCmd.Flags().StringSlice(
		"only", nil,
		"Stages to run: subjects,photos,labels,albums,markers,thumbs (default all)")

	_ = migrateFromPhotoPrismCmd.MarkFlagRequired("pp-db")
	_ = migrateFromPhotoPrismCmd.MarkFlagRequired("pp-originals")
}

func runMigrateFromPhotoPrism(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := config.Load()
	ppDB := mustGetString(cmd, "pp-db")
	ppOriginals := mustGetString(cmd, "pp-originals")
	ppCache := mustGetString(cmd, "pp-cache")
	uploaderUsername := mustGetString(cmd, "uploader-username")
	dryRun := mustGetBool(cmd, "dry-run")
	skipThumbs := mustGetBool(cmd, "skip-thumbs")
	batchSize := mustGetInt(cmd, "batch-size")
	concurrency := mustGetInt(cmd, "concurrency")
	only := mustGetStringSlice(cmd, "only")

	deps, cleanup, err := openMigrateDeps(ctx, cfg, ppDB)
	if err != nil {
		return err
	}
	defer cleanup()

	uploaderUID, err := resolveUploaderUID(ctx, deps.users, uploaderUsername)
	if err != nil {
		return err
	}

	opts := &migrate.Options{
		MariaDB:       deps.mariaDB,
		OriginalsRoot: ppOriginals,
		CacheRoot:     ppCache,
		UploaderUID:   uploaderUID,
		DryRun:        dryRun,
		SkipThumbs:    skipThumbs,
		BatchSize:     batchSize,
		Concurrency:   concurrency,
		Only:          only,
		Store:         deps.store,
		Photos:        deps.photos,
		Subjects:      deps.subjects,
		Labels:        deps.labels,
		Albums:        deps.albums,
		Markers:       deps.markers,
		Writer:        cmd.OutOrStdout(),
	}

	start := time.Now()
	report, err := migrate.Run(ctx, opts)
	dur := time.Since(start)
	if report != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Elapsed: %s\n", dur.Round(time.Second))
	}
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// migrateDeps bundles every dependency the migrator needs. Building them
// up-front keeps runMigrateFromPhotoPrism short enough for the lint cap
// and lets the test suite swap in fakes.
type migrateDeps struct {
	mariaDB  *sql.DB
	store    *storage.Storage
	photos   database.PhotoWriter
	subjects database.SubjectWriter
	labels   database.LabelWriter
	albums   database.AlbumWriter
	markers  database.MarkerWriter
	users    database.UserReader
}

// openMigrateDeps sets up the MariaDB handle, the destination Postgres
// pool + repositories, and the storage layer. The returned cleanup
// closes both DB handles.
func openMigrateDeps(_ context.Context, cfg *config.Config, ppDSN string) (*migrateDeps, func(), error) {
	if cfg.Database.URL == "" {
		return nil, nil, errors.New("DATABASE_URL is required")
	}
	if cfg.Storage.OriginalsPath == "" || cfg.Storage.CachePath == "" {
		return nil, nil, errors.New("STORAGE_ORIGINALS_PATH and STORAGE_CACHE_PATH are required")
	}

	mariaDB, err := sql.Open("mysql", ppDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open mariadb: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mariaDB.PingContext(pingCtx); err != nil {
		_ = mariaDB.Close()
		return nil, nil, fmt.Errorf("ping mariadb: %w", err)
	}

	if err := postgres.Initialize(&cfg.Database); err != nil {
		_ = mariaDB.Close()
		return nil, nil, fmt.Errorf("initialise postgres: %w", err)
	}
	pool := postgres.GetGlobalPool()

	store, err := storage.New(cfg.Storage.OriginalsPath, cfg.Storage.CachePath)
	if err != nil {
		_ = mariaDB.Close()
		return nil, nil, fmt.Errorf("open storage: %w", err)
	}

	cleanup := func() {
		_ = mariaDB.Close()
		_ = pool.Close()
	}

	return &migrateDeps{
		mariaDB:  mariaDB,
		store:    store,
		photos:   postgres.NewPhotoRepository(pool),
		subjects: postgres.NewSubjectRepository(pool),
		labels:   postgres.NewLabelRepository(pool),
		albums:   postgres.NewAlbumRepository(pool),
		markers:  postgres.NewMarkerRepository(pool),
		users:    postgres.NewUserRepository(pool),
	}, cleanup, nil
}

// resolveUploaderUID returns the native user UID for the requested
// username. An empty username is allowed — the migration just writes
// NULL into photos.uploaded_by.
func resolveUploaderUID(ctx context.Context, users database.UserReader, username string) (string, error) {
	if username == "" {
		return "", nil
	}
	u, err := users.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return "", fmt.Errorf(
				"--uploader-username %q does not exist; create it first with the admin tooling",
				username,
			)
		}
		return "", fmt.Errorf("look up uploader %q: %w", username, err)
	}
	return u.UID, nil
}
