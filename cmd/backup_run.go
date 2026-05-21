package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// executeBackup writes the timestamped backup directory atomically.
//
// The flow is:
//  1. Create a `.photo-sorter-<ts>.tmp` directory under --output.
//  2. Run the originals tar (unless --skip-originals).
//  3. Run pg_dump (unless --skip-db).
//  4. Write metadata.json.
//  5. Rename the tmp directory to its final `photo-sorter-<ts>` name.
//
// On failure the tmp directory is left in place for inspection unless
// --cleanup-on-failure was passed.
func executeBackup(ctx context.Context, opts backupOptions) error {
	if err := os.MkdirAll(opts.output, 0o750); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	ts := time.Now().UTC().Format(backupTimestampLayout)
	tmpDir := tmpDirPath(opts.output, ts)
	finalDir := finalDirPath(opts.output, ts)

	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return fmt.Errorf("creating tmp dir: %w", err)
	}

	result, err := runBackupSteps(ctx, opts, tmpDir)
	if err != nil {
		handleBackupFailure(tmpDir, opts.cleanupOnFailure)
		return err
	}

	if err := writeMetadataFile(tmpDir, result); err != nil {
		handleBackupFailure(tmpDir, opts.cleanupOnFailure)
		return fmt.Errorf("writing metadata.json: %w", err)
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		handleBackupFailure(tmpDir, opts.cleanupOnFailure)
		return fmt.Errorf("renaming %s -> %s: %w", tmpDir, finalDir, err)
	}

	printBackupSummary(finalDir, result)
	return nil
}

// runBackupSteps runs the configured artifact-producing steps (tar, pg_dump)
// inside the tmp directory and returns the populated result.
func runBackupSteps(ctx context.Context, opts backupOptions, tmpDir string) (backupResult, error) {
	result := backupResult{
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		SorterVersion: Version,
	}

	if !opts.skipOriginals {
		archive := filepath.Join(tmpDir, "originals.tar."+opts.compressor.compressedExtension())
		summary, err := archiveOriginals(ctx, opts.originalsPath, archive, opts.compressor, opts.progressEvery)
		if err != nil {
			return result, fmt.Errorf("archiving originals: %w", err)
		}
		result.OriginalsBytes = summary.totalBytes
		result.FileCount = summary.fileCount
	}

	if !opts.skipDB {
		dump := filepath.Join(tmpDir, "db.sql."+opts.compressor.compressedExtension())
		size, err := dumpDatabase(ctx, opts.dbURL, dump, opts.compressor)
		if err != nil {
			return result, fmt.Errorf("dumping database: %w", err)
		}
		result.DBSizeBytes = size
	}

	return result, nil
}

// writeMetadataFile serialises the result as pretty-printed JSON inside the
// tmp directory.
func writeMetadataFile(tmpDir string, result backupResult) error {
	path := filepath.Join(tmpDir, "metadata.json")
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// handleBackupFailure removes the tmp directory when the user asked for
// cleanup, otherwise leaves it in place and prints its path so the operator
// can inspect partial artifacts.
func handleBackupFailure(tmpDir string, cleanup bool) {
	if cleanup {
		_ = os.RemoveAll(tmpDir)
		return
	}
	fmt.Fprintf(os.Stderr, "Backup failed; tmp directory preserved at %s\n", tmpDir)
}

// printBackupSummary writes a short human-readable summary to stdout.
func printBackupSummary(finalDir string, result backupResult) {
	fmt.Printf("\nBackup complete: %s\n", finalDir)
	fmt.Printf("  Sorter version:  %s\n", result.SorterVersion)
	fmt.Printf("  Created at:      %s\n", result.CreatedAt)
	if result.DBSizeBytes > 0 {
		fmt.Printf("  Database dump:   %d bytes\n", result.DBSizeBytes)
	}
	if result.FileCount > 0 {
		fmt.Printf("  Originals:       %d files, %d bytes\n", result.FileCount, result.OriginalsBytes)
	}
}
