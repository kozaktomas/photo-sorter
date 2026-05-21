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
one-shot migration moved everything correctly. The verifier runs two
phases:

  1. structural — existence, counts, on-disk orphans, marker geometry.
     Each section reports counts first and the first 50 missing items.

  2. field-level — every column the migrator is supposed to copy is
     compared cell-by-cell (photos: 30+ columns including keywords,
     GPS, EXIF, flags; subjects: bio/about/alias/type/favorite/private;
     labels: description/categories/priority/favorite; albums:
     description/location/category/notes/filter/order/favorite/private/
     type; markers: score/invalid/reviewed/subject_uid). The JSON
     output exposes a field_diffs[] array per entity; the text output
     prints a header summarising the per-field counts and the first 50
     individual diffs.

Tolerance bands tolerate 1-second drift on taken_at, 1e-6 on lat/lng,
1 m on altitude, and 0.01 on marker score by default; --strict treats
them as diffs.

The exit code is 0 when no differences are found and 1 when at least
one diff is reported, so the command can be chained into automated
post-migration checks. Zero diffs from migrate-verify is the
authoritative gate for cancelling the PhotoPrism + MariaDB compose
services.

Examples:
  # Human-readable report with ANSI colour.
  photo-sorter migrate-verify \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals

  # Skip the slow rehash pass and only run the field-level diff.
  photo-sorter migrate-verify \
      --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
      --pp-originals /photoprism/originals \
      --fields-only

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
	migrateVerifyCmd.Flags().Bool("fields-only", false,
		"Skip the structural existence/disk pass and run only the field-level diff")
	migrateVerifyCmd.Flags().Bool("strict", false,
		"Treat tolerance-band differences (1s on dates, 1e-6 on coords, 1m altitude, 0.01 score) as diffs")
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
	fieldsOnly := mustGetBool(cmd, "fields-only")
	strict := mustGetBool(cmd, "strict")

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
		FieldsOnly:    fieldsOnly,
		Strict:        strict,
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
