package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// dumpDatabase invokes the pg_dump binary against dbURL and writes the
// compressed plain-format dump to outputPath. The size of the compressed
// file in bytes is returned on success.
//
// The subprocess is given a 30-minute timeout (see pgDumpTimeout) and its
// stderr is captured so it can be surfaced when the dump fails.
func dumpDatabase(parent context.Context, dbURL, outputPath string, comp backupCompressor) (int64, error) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return 0, fmt.Errorf("pg_dump binary not found in PATH: %w", err)
	}

	out, err := os.Create(outputPath) //nolint:gosec // path is constructed by the backup orchestrator
	if err != nil {
		return 0, fmt.Errorf("creating dump file: %w", err)
	}
	defer out.Close()

	cw, err := newCompressingWriter(out, comp)
	if err != nil {
		return 0, err
	}

	if err := runPgDump(parent, dbURL, cw); err != nil {
		_ = cw.Close()
		return 0, err
	}

	// Close compressor explicitly so the final bytes are flushed before we
	// stat the output file for its size.
	if err := cw.Close(); err != nil {
		return 0, fmt.Errorf("closing compressor: %w", err)
	}
	if err := out.Sync(); err != nil {
		return 0, fmt.Errorf("syncing dump file: %w", err)
	}
	info, err := out.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat dump file: %w", err)
	}
	fmt.Printf("Database dump complete: %d bytes (compressed)\n", info.Size())
	return info.Size(), nil
}

// runPgDump executes the pg_dump subprocess, streaming stdout into dest and
// surfacing stderr in the returned error when the process fails.
func runPgDump(parent context.Context, dbURL string, dest io.Writer) error {
	ctx, cancel := context.WithTimeout(parent, pgDumpTimeout)
	defer cancel()

	fmt.Println("Running pg_dump ...")
	// #nosec G204 -- dbURL is operator-supplied via --db-url or $DATABASE_URL;
	// pg_dump receives it via a single --dbname=<URL> argument with no shell
	// interpolation, so there is no command-injection surface here.
	cmd := exec.CommandContext(ctx,
		"pg_dump",
		"--format=plain",
		"--no-owner",
		"--no-privileges",
		"--dbname="+dbURL,
	)
	var stderr bytes.Buffer
	cmd.Stdout = dest
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("pg_dump timed out after %s", pgDumpTimeout)
		}
		trimmed := bytes.TrimSpace(stderr.Bytes())
		if len(trimmed) > 0 {
			return fmt.Errorf("pg_dump failed: %w (stderr: %s)", err, trimmed)
		}
		return fmt.Errorf("pg_dump failed: %w", err)
	}
	return nil
}
