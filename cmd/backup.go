package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// backupTimestampLayout is the layout for the timestamped backup directory name.
// Sorts lexically in chronological order so retention pruning can sort by name.
const backupTimestampLayout = "20060102-150405"

// backupDirPrefix is the prefix used for finished backup directories.
const backupDirPrefix = "photo-sorter-"

// backupTmpPrefix is the prefix used for in-flight backup directories.
// A leading dot keeps them hidden from casual `ls` output and prevents them
// from being matched by the final-directory glob used during retention.
const backupTmpPrefix = ".photo-sorter-"

// defaultProgressEvery is the default cadence for originals progress logs.
const defaultProgressEvery = 500

// pgDumpTimeout bounds the pg_dump subprocess. Tuned generously for large
// vector-heavy databases on slow disks.
const pgDumpTimeout = 30 * time.Minute

// backupCompressor enumerates the supported compression algorithms.
type backupCompressor string

const (
	compressorZstd backupCompressor = "zstd"
	compressorGzip backupCompressor = "gzip"
)

// backupOptions captures the parsed CLI flags for a single backup invocation.
type backupOptions struct {
	output           string
	originalsPath    string
	dbURL            string
	keep             int
	compressor       backupCompressor
	skipOriginals    bool
	skipDB           bool
	cleanupOnFailure bool
	progressEvery    int
}

// backupResult is the structured summary printed at the end of a run and
// embedded inside the metadata.json file.
type backupResult struct {
	CreatedAt      string `json:"created_at"`
	SorterVersion  string `json:"sorter_version"`
	DBSizeBytes    int64  `json:"db_size_bytes"`
	OriginalsBytes int64  `json:"originals_bytes"`
	FileCount      int    `json:"file_count"`
}

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create a timestamped backup of the originals tree and the Postgres database",
	Long: `Create a single timestamped backup containing the originals directory
and a pg_dump of the photo-sorter Postgres database. The thumbnail cache is
intentionally excluded because it can be regenerated from the originals.

The backup is written atomically into <output>/photo-sorter-<YYYYMMDD-HHMMSS>/
with three artifacts:
  metadata.json        Summary (created_at, version, sizes, file count)
  db.sql.(zst|gz)      Compressed plain-format pg_dump
  originals.tar.(zst|gz)  Compressed tar of the originals tree

Examples:
  # Daily backup keeping the last 14 runs.
  photo-sorter backup --output /var/backups/photo-sorter --keep 14

  # Originals only (no database access).
  photo-sorter backup --output /tmp/bak --skip-db

  # Database only (no large file scan).
  photo-sorter backup --output /tmp/bak --skip-originals`,
	RunE: runBackup,
}

func init() {
	rootCmd.AddCommand(backupCmd)

	backupCmd.Flags().String("output", "", "Output directory for backups (required)")
	backupCmd.Flags().String("originals-path", "", "Path to the originals directory (defaults to $STORAGE_ORIGINALS_PATH)")
	backupCmd.Flags().String("db-url", "", "Postgres connection URL (defaults to $DATABASE_URL)")
	backupCmd.Flags().Int("keep", 14, "Number of backups to retain (0 disables pruning)")
	backupCmd.Flags().String("compress", "zstd", "Compression algorithm: zstd or gzip")
	backupCmd.Flags().Bool("skip-originals", false, "Skip the originals tar")
	backupCmd.Flags().Bool("skip-db", false, "Skip the pg_dump")
	backupCmd.Flags().Bool("cleanup-on-failure", false, "Remove the .tmp directory if the run fails")
	backupCmd.Flags().Int("progress-every", defaultProgressEvery, "Originals progress cadence (files between log lines)")

	if err := backupCmd.MarkFlagRequired("output"); err != nil {
		panic(fmt.Sprintf("marking --output required: %v", err))
	}
}

// runBackup is the Cobra RunE entrypoint. It parses flags and delegates
// to executeBackup, then runs retention pruning on success.
func runBackup(cmd *cobra.Command, _ []string) error {
	opts, err := parseBackupOptions(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if err := executeBackup(ctx, opts); err != nil {
		return err
	}

	if opts.keep > 0 {
		pruned, err := pruneBackups(opts.output, opts.keep)
		if err != nil {
			return fmt.Errorf("retention prune: %w", err)
		}
		if pruned > 0 {
			fmt.Printf("Pruned %d old backup(s) past --keep=%d.\n", pruned, opts.keep)
		}
	}

	return nil
}

// parseBackupOptions reads the Cobra flags and resolves environment defaults.
// It returns an error if a required value is missing or an enum is invalid.
func parseBackupOptions(cmd *cobra.Command) (backupOptions, error) {
	opts := readBackupFlags(cmd)
	applyBackupDefaults(&opts)

	compressFlag := mustGetString(cmd, "compress")
	switch backupCompressor(compressFlag) {
	case compressorZstd, compressorGzip:
		opts.compressor = backupCompressor(compressFlag)
	default:
		return opts, fmt.Errorf("invalid --compress value %q (want zstd or gzip)", compressFlag)
	}

	if err := validateBackupOptions(opts); err != nil {
		return opts, err
	}
	return opts, nil
}

// readBackupFlags pulls every CLI-supplied value into a backupOptions struct
// without applying environment defaults or validation.
func readBackupFlags(cmd *cobra.Command) backupOptions {
	return backupOptions{
		output:           mustGetString(cmd, "output"),
		originalsPath:    mustGetString(cmd, "originals-path"),
		dbURL:            mustGetString(cmd, "db-url"),
		keep:             mustGetInt(cmd, "keep"),
		skipOriginals:    mustGetBool(cmd, "skip-originals"),
		skipDB:           mustGetBool(cmd, "skip-db"),
		cleanupOnFailure: mustGetBool(cmd, "cleanup-on-failure"),
		progressEvery:    mustGetInt(cmd, "progress-every"),
	}
}

// applyBackupDefaults fills in environment-driven fallbacks for fields the
// user did not pass on the command line.
func applyBackupDefaults(opts *backupOptions) {
	if opts.progressEvery <= 0 {
		opts.progressEvery = defaultProgressEvery
	}
	if opts.originalsPath == "" {
		opts.originalsPath = os.Getenv("STORAGE_ORIGINALS_PATH")
	}
	if opts.dbURL == "" {
		opts.dbURL = os.Getenv("DATABASE_URL")
	}
}

// validateBackupOptions returns a descriptive error if the option combination
// is internally inconsistent or missing a required value.
func validateBackupOptions(opts backupOptions) error {
	if opts.skipOriginals && opts.skipDB {
		return errors.New("--skip-originals and --skip-db cannot both be set")
	}
	if !opts.skipOriginals && opts.originalsPath == "" {
		return errors.New("originals path is required (set --originals-path or $STORAGE_ORIGINALS_PATH)")
	}
	if !opts.skipDB && opts.dbURL == "" {
		return errors.New("database URL is required (set --db-url or $DATABASE_URL)")
	}
	return nil
}

// compressedExtension returns the file extension corresponding to the chosen
// compressor, e.g. "zst" or "gz".
func (c backupCompressor) compressedExtension() string {
	if c == compressorGzip {
		return "gz"
	}
	return "zst"
}

// finalDirPath returns the path of a completed backup directory for a given
// timestamp under output.
func finalDirPath(output, ts string) string {
	return filepath.Join(output, backupDirPrefix+ts)
}

// tmpDirPath returns the path of an in-flight backup directory for a given
// timestamp under output.
func tmpDirPath(output, ts string) string {
	return filepath.Join(output, backupTmpPrefix+ts+".tmp")
}
