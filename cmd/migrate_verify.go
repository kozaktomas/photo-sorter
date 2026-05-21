package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/verify"
	"github.com/spf13/cobra"
)

var migrateVerifyCmd = &cobra.Command{
	Use:   "migrate-verify",
	Short: "Compare PhotoPrism against the sorter database after a migration",
	Long: `Run a read-only diff between an existing PhotoPrism instance (MariaDB
+ originals tree) and the photo-sorter native database to confirm a
one-shot migration moved everything correctly. Each section reports
counts first and then the first 50 missing items per category.

The exit code is 0 when no differences are found and 1 when at least one
diff is reported, so the command can be chained into automated post-
migration checks.

Examples:
  # Human-readable report with ANSI colour.
  photo-sorter migrate-verify \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals

  # Machine-readable JSON, for jq/grafana/CI.
  photo-sorter migrate-verify \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals \
      --json > verify.json

  # Disable colour for log files / non-TTY consumers.
  photo-sorter migrate-verify \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals \
      --no-color`,
	RunE: runMigrateVerify,
}

func init() {
	rootCmd.AddCommand(migrateVerifyCmd)
	migrateVerifyCmd.Flags().String("pp-db", "",
		"PhotoPrism MariaDB DSN, e.g. user:pw@tcp(host:3306)/photoprism (required)")
	migrateVerifyCmd.Flags().String("pp-originals", "",
		"Path to the PhotoPrism originals directory (required)")
	migrateVerifyCmd.Flags().Bool("json", false,
		"Emit a machine-readable JSON report instead of the human-readable text")
	migrateVerifyCmd.Flags().Bool("no-color", false,
		"Disable ANSI colour escapes in the human-readable report")
	migrateVerifyCmd.Flags().Int("concurrency", verify.DefaultConcurrency,
		"Number of parallel workers for the photo hash pass")
	_ = migrateVerifyCmd.MarkFlagRequired("pp-db")
	_ = migrateVerifyCmd.MarkFlagRequired("pp-originals")
}

// runMigrateVerify wires the CLI flags into a verify.Options, runs the
// verifier, formats the report, and exits 1 when any diff is reported so
// the command can be chained into automated post-migration checks.
func runMigrateVerify(cmd *cobra.Command, _ []string) error {
	hadDiffs, err := executeVerify(cmd)
	if err != nil {
		return err
	}
	if hadDiffs {
		// Non-zero exit lets shell scripts chain `migrate-verify` into
		// other automation without parsing stdout. We are past every
		// defer (executeVerify returned cleanly), so it is safe to
		// terminate the process here.
		os.Exit(1)
	}
	return nil
}

// executeVerify owns the lifecycle of the verifier dependencies (so the
// deferred cleanup runs even when the report has diffs) and returns
// (hadDiffs, error) to runMigrateVerify, which then decides whether to
// os.Exit(1).
func executeVerify(cmd *cobra.Command) (bool, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := config.Load()
	ppDB := mustGetString(cmd, "pp-db")
	ppOriginals := mustGetString(cmd, "pp-originals")
	jsonOut := mustGetBool(cmd, "json")
	noColour := mustGetBool(cmd, "no-color")
	concurrency := mustGetInt(cmd, "concurrency")

	deps, cleanup, err := openVerifyDeps(cfg, ppDB)
	if err != nil {
		return false, err
	}
	defer cleanup()

	opts := &verify.Options{
		MariaDB:       deps.mariaDB,
		OriginalsRoot: ppOriginals,
		Store:         deps.store,
		Photos:        deps.photos,
		Albums:        deps.albums,
		Labels:        deps.labels,
		Subjects:      deps.subjects,
		Markers:       deps.markers,
		Concurrency:   concurrency,
	}
	if !jsonOut {
		opts.Writer = cmd.OutOrStdout()
	}

	report, err := verify.Run(ctx, opts)
	if err != nil {
		return false, fmt.Errorf("verify: %w", err)
	}

	if jsonOut {
		if err := verify.FormatJSON(cmd.OutOrStdout(), report); err != nil {
			return false, fmt.Errorf("format json: %w", err)
		}
	} else {
		verify.FormatText(cmd.OutOrStdout(), report, !noColour)
	}
	return report.HasDiffs(), nil
}

// verifyDeps bundles every dependency the verifier needs. The
// constructor builds them up-front so runMigrateVerify stays inside the
// linter's complexity budget and the test suite can swap in fakes.
type verifyDeps struct {
	mariaDB  *sql.DB
	store    *storage.Storage
	photos   *postgres.PhotoRepository
	albums   *postgres.AlbumRepository
	labels   *postgres.LabelRepository
	subjects *postgres.SubjectRepository
	markers  *postgres.MarkerRepository
}

// openVerifyDeps sets up the MariaDB handle, the destination Postgres
// pool + repositories, and the storage layer. The returned cleanup
// closes both DB handles.
func openVerifyDeps(cfg *config.Config, ppDSN string) (*verifyDeps, func(), error) {
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
		_ = pool.Close()
		return nil, nil, fmt.Errorf("open storage: %w", err)
	}

	cleanup := func() {
		_ = mariaDB.Close()
		_ = pool.Close()
	}
	return &verifyDeps{
		mariaDB:  mariaDB,
		store:    store,
		photos:   postgres.NewPhotoRepository(pool),
		albums:   postgres.NewAlbumRepository(pool),
		labels:   postgres.NewLabelRepository(pool),
		subjects: postgres.NewSubjectRepository(pool),
		markers:  postgres.NewMarkerRepository(pool),
	}, cleanup, nil
}
