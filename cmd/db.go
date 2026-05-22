package cmd

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

// dbExportTimestampLayout is the timestamp format used in default output
// filenames. UTC, sortable, dash-separated.
const dbExportTimestampLayout = "20060102-150405"

// pgDumpMagic is the first 5 bytes of a PostgreSQL custom-format dump.
const pgDumpMagic = "PGDMP"

// gzipMagic is the gzip file header's first two bytes.
var gzipMagic = []byte{0x1f, 0x8b}

// yesAnswerLiteral is the literal confirmation token required by the
// db-import destructive prompt.
const yesAnswerLiteral = "yes"

// formatCustom and formatPlain are the two pg_dump output formats this
// command understands.
const (
	formatCustom = "custom"
	formatPlain  = "plain"
)

// maxDecompressedDumpBytes caps the size we are willing to gunzip into a
// temp file before invoking pg_restore. 50 GiB is well above any
// realistic photosorter dump, and protects against a malicious gzip bomb
// pointed at us via --input.
const maxDecompressedDumpBytes = int64(50) << 30

var dbExportCmd = &cobra.Command{
	Use:   "db-export",
	Short: "Dump the photo-sorter PostgreSQL database to a single file",
	Long: `Dump the photo-sorter PostgreSQL database to a single file via pg_dump.

This command covers the metadata side of disaster recovery: embeddings,
faces, books, users, sessions, era_embeddings, photos, albums, labels,
markers, subjects — every row that lives in the photosorter database.

Photos themselves (the originals tree on disk) are NOT part of this
command. Back those up separately via rsync/borg/etc.

The 'custom' format is recommended: it is pg_dump's native compressed
binary format, supports parallel and selective restores, and is the
input format pg_restore expects. Use 'plain' if you need a human-readable
SQL file that can be applied via psql.`,
	RunE: runDBExport,
}

var dbImportCmd = &cobra.Command{
	Use:   "db-import",
	Short: "Restore a photo-sorter PostgreSQL database from a dump file",
	Long: `Restore a photo-sorter PostgreSQL database from a dump previously
created by db-export (or any equivalent pg_dump output).

The dump format is detected automatically from the file header:
  * PostgreSQL custom format (-Fc) — restored via pg_restore.
  * Plain SQL (-Fp) — streamed into psql stdin.

Gzipped dumps (with or without an outer .gz extension) are detected and
decompressed transparently.

If the target database already contains data (the 'embeddings' table has
rows), the command refuses to overwrite it without --yes.`,
	RunE: runDBImport,
}

func init() {
	rootCmd.AddCommand(dbExportCmd)
	rootCmd.AddCommand(dbImportCmd)

	dbExportCmd.Flags().StringP("output", "o", "",
		"Destination file path (default: photosorter-<UTC timestamp>.<ext>)")
	dbExportCmd.Flags().String("format", formatCustom,
		"Dump format: 'custom' (recommended) or 'plain' (SQL)")
	dbExportCmd.Flags().Bool("no-compress", false,
		"For 'plain' format, skip gzipping the output (ignored for 'custom')")
	dbExportCmd.Flags().Bool("force", false, "Overwrite the output file if it already exists")

	dbImportCmd.Flags().StringP("input", "i", "", "Source dump file (required)")
	dbImportCmd.Flags().BoolP("yes", "y", false, "Skip the interactive confirmation prompt")
	dbImportCmd.Flags().Bool("drop-existing", false,
		"DROP SCHEMA public CASCADE; CREATE SCHEMA public; before restoring (clean slate)")
	if err := dbImportCmd.MarkFlagRequired("input"); err != nil {
		panic(fmt.Sprintf("marking --input required: %v", err))
	}
}

// dbExportOptions holds the parsed flags for a single db-export invocation.
type dbExportOptions struct {
	output     string
	format     string
	noCompress bool
	force      bool
}

// dbImportOptions holds the parsed flags for a single db-import invocation.
type dbImportOptions struct {
	input        string
	yes          bool
	dropExisting bool
}

// runDBExport is the Cobra RunE entrypoint for db-export.
func runDBExport(cmd *cobra.Command, _ []string) error {
	opts, dbURL, err := prepareDBExport(cmd)
	if err != nil {
		return err
	}

	start := time.Now()
	if err := writeDBExport(cmd.Context(), dbURL, opts); err != nil {
		_ = os.Remove(opts.output)
		return err
	}

	info, err := os.Stat(opts.output)
	if err != nil {
		return fmt.Errorf("stat output: %w", err)
	}
	fmt.Printf("\nDump complete.\n")
	fmt.Printf("  Path:    %s\n", opts.output)
	fmt.Printf("  Size:    %s\n", humanBytes(info.Size()))
	fmt.Printf("  Elapsed: %s\n", time.Since(start).Round(time.Second))
	return nil
}

// prepareDBExport bundles the up-front validation (flag parsing, DB URL
// check, binary lookup, output-path collision check, extension warning)
// so runDBExport stays under the cyclomatic-complexity budget.
func prepareDBExport(cmd *cobra.Command) (dbExportOptions, string, error) {
	opts, err := parseDBExportOptions(cmd)
	if err != nil {
		return opts, "", err
	}

	dbURL, err := requireDatabaseURL()
	if err != nil {
		return opts, "", err
	}

	if _, err := exec.LookPath("pg_dump"); err != nil {
		return opts, "", errors.New("pg_dump not found in PATH — install postgresql-client")
	}

	if opts.output == "" {
		opts.output = defaultExportPath(opts)
	}

	if !opts.force {
		if _, err := os.Stat(opts.output); err == nil {
			return opts, "", fmt.Errorf("output file %s already exists (use --force to overwrite)", opts.output)
		} else if !errors.Is(err, os.ErrNotExist) {
			return opts, "", fmt.Errorf("stat output: %w", err)
		}
	}

	if opts.format == formatCustom && strings.HasSuffix(strings.ToLower(opts.output), ".sql") {
		fmt.Fprintf(os.Stderr,
			"warning: --output ends in .sql but --format=custom is binary (consider .dump or .pgcustom)\n")
	}
	return opts, dbURL, nil
}

// runDBImport is the Cobra RunE entrypoint for db-import.
func runDBImport(cmd *cobra.Command, _ []string) error {
	opts, dbURL, format, gzipped, err := prepareDBImport(cmd)
	if err != nil {
		return err
	}

	if err := confirmDestructiveImport(dbURL, opts); err != nil {
		return err
	}

	if opts.dropExisting {
		if err := dropPublicSchema(cmd.Context(), dbURL); err != nil {
			return fmt.Errorf("drop public schema: %w", err)
		}
	}

	if err := executeDBImport(cmd.Context(), dbURL, opts, format, gzipped); err != nil {
		return err
	}

	printImportNextSteps()
	return nil
}

// prepareDBImport handles the up-front validation steps for db-import:
// flag parsing, DB URL check, input existence, dump-format detection,
// and the format-specific binary lookup. Splitting these off keeps
// runDBImport under the cyclomatic-complexity budget.
func prepareDBImport(cmd *cobra.Command) (dbImportOptions, string, string, bool, error) {
	opts := dbImportOptions{
		input:        mustGetString(cmd, "input"),
		yes:          mustGetBool(cmd, "yes"),
		dropExisting: mustGetBool(cmd, "drop-existing"),
	}

	dbURL, err := requireDatabaseURL()
	if err != nil {
		return opts, "", "", false, err
	}

	if _, err := os.Stat(opts.input); err != nil {
		return opts, "", "", false, fmt.Errorf("input file %s: %w", opts.input, err)
	}

	format, gzipped, err := detectDumpFormat(opts.input)
	if err != nil {
		return opts, "", "", false, fmt.Errorf("detect dump format: %w", err)
	}

	if err := requireRestoreBinary(format); err != nil {
		return opts, "", "", false, err
	}
	return opts, dbURL, format, gzipped, nil
}

// requireRestoreBinary verifies that the binary matching the detected
// dump format is reachable on PATH and returns an actionable error if
// not.
func requireRestoreBinary(format string) error {
	switch format {
	case formatCustom:
		if _, err := exec.LookPath("pg_restore"); err != nil {
			return errors.New("pg_restore not found in PATH — install postgresql-client")
		}
	case formatPlain:
		if _, err := exec.LookPath("psql"); err != nil {
			return errors.New("psql not found in PATH — install postgresql-client")
		}
	}
	return nil
}

// parseDBExportOptions reads the db-export flags and validates the format
// enum. It does not fill the default output path; that happens after we
// have validated the format (so the default extension matches the format).
func parseDBExportOptions(cmd *cobra.Command) (dbExportOptions, error) {
	opts := dbExportOptions{
		output:     mustGetString(cmd, "output"),
		format:     mustGetString(cmd, "format"),
		noCompress: mustGetBool(cmd, "no-compress"),
		force:      mustGetBool(cmd, "force"),
	}
	switch opts.format {
	case formatCustom, formatPlain:
	default:
		return opts, fmt.Errorf("invalid --format %q (want 'custom' or 'plain')", opts.format)
	}
	return opts, nil
}

// defaultExportPath returns the auto-generated output filename used when
// --output was not provided. The extension matches the chosen format so
// the file's contents are not misrepresented.
func defaultExportPath(opts dbExportOptions) string {
	ts := time.Now().UTC().Format(dbExportTimestampLayout)
	switch opts.format {
	case formatCustom:
		return fmt.Sprintf("photosorter-%s.dump", ts)
	default: // plain
		if opts.noCompress {
			return fmt.Sprintf("photosorter-%s.sql", ts)
		}
		return fmt.Sprintf("photosorter-%s.sql.gz", ts)
	}
}

// requireDatabaseURL pulls DATABASE_URL from the environment and validates
// that it parses as a Postgres URL. The libpq CLIs accept it as a
// positional argument, sidestepping the PG* env-var dance.
func requireDatabaseURL() (string, error) {
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		return "", errors.New("DATABASE_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("DATABASE_URL is not a valid URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("DATABASE_URL has unsupported scheme %q (want postgres://)", u.Scheme)
	}
	return raw, nil
}

// writeDBExport invokes pg_dump and writes the (optionally gzipped) output
// to opts.output. The child's stderr is streamed live so users see
// progress on long dumps.
func writeDBExport(ctx context.Context, dbURL string, opts dbExportOptions) error {
	out, err := os.Create(opts.output)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer out.Close()

	writer := exportWriter(out, opts)
	fmt.Printf("Running pg_dump --format=%s ...\n", opts.format)
	if err := runPgDumpToWriter(ctx, dbURL, opts.format, writer); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing compressor: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("syncing output: %w", err)
	}
	return nil
}

// exportWriter returns the io.WriteCloser pg_dump's stdout will be copied
// into. For 'plain' format without --no-compress it wraps the file in a
// gzip writer; for everything else it is a no-op closer around the file.
func exportWriter(out io.Writer, opts dbExportOptions) io.WriteCloser {
	if opts.format == formatPlain && !opts.noCompress {
		return gzip.NewWriter(out)
	}
	return nopWriteCloser{out}
}

// runPgDumpToWriter spawns pg_dump with the chosen format and streams its
// stdout into dest. The child's stderr is wired to the user's terminal
// so progress lines flow live.
func runPgDumpToWriter(ctx context.Context, dbURL, format string, dest io.Writer) error {
	args := []string{"--no-owner", "--no-privileges", "--format=" + format, dbURL}
	// #nosec G204 -- dbURL is operator-supplied via DATABASE_URL and
	// passed as a single positional argument with no shell interpolation.
	pgDump := exec.CommandContext(ctx, "pg_dump", args...)
	pgDump.Stderr = os.Stderr
	pgDump.Stdout = dest
	if err := pgDump.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w", err)
	}
	return nil
}

// nopWriteCloser turns an io.Writer into an io.WriteCloser whose Close is
// a no-op. We use it for the uncompressed path so the writer interface
// is uniform across formats.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// detectDumpFormat inspects the first bytes of path and returns the
// detected pg_dump format ("custom" or "plain") and whether the on-disk
// file is gzipped. It transparently peeks through a gzip layer.
func detectDumpFormat(path string) (format string, gzipped bool, err error) {
	probe, gzipped, err := readDumpProbe(path)
	if err != nil {
		return "", gzipped, err
	}

	switch {
	case len(probe) >= len(pgDumpMagic) && bytes.Equal(probe[:len(pgDumpMagic)], []byte(pgDumpMagic)):
		return formatCustom, gzipped, nil
	case looksLikePlainSQL(probe):
		return formatPlain, gzipped, nil
	default:
		return "", gzipped, fmt.Errorf(
			"unrecognized dump format (first bytes: %q); expected PGDMP custom dump or plain SQL", probe)
	}
}

// readDumpProbe reads the leading bytes of path, transparently decoding a
// gzip wrapper if present, so the caller can sniff the inner pg_dump
// magic without caring about the on-disk layer.
func readDumpProbe(path string) ([]byte, bool, error) {
	// #nosec G304 -- path is the operator-supplied --input flag; opening
	// arbitrary files is the documented purpose of this command.
	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("opening dump: %w", err)
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, fmt.Errorf("reading dump header: %w", err)
	}
	head = head[:n]

	if len(head) < 2 || !bytes.Equal(head[:2], gzipMagic) {
		return head, false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, true, fmt.Errorf("rewinding dump: %w", err)
	}
	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, true, fmt.Errorf("gzip header invalid: %w", err)
	}
	defer gr.Close()
	buf := make([]byte, 16)
	m, err := io.ReadFull(gr, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, true, fmt.Errorf("reading gzip-decoded header: %w", err)
	}
	return buf[:m], true, nil
}

// looksLikePlainSQL is a very loose check: the first non-whitespace
// content of a plain pg_dump output is typically a comment ("--") or a
// SET statement, but psql will happily accept BEGIN, CREATE, etc.
func looksLikePlainSQL(probe []byte) bool {
	trimmed := bytes.TrimLeft(probe, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	prefixes := []string{"--", "SET ", "BEGIN", "CREATE", "ALTER", "COPY", "INSERT", "\\connect"}
	upper := strings.ToUpper(string(trimmed))
	for _, p := range prefixes {
		if strings.HasPrefix(upper, strings.ToUpper(p)) {
			return true
		}
	}
	return false
}

// confirmDestructiveImport checks the target database for existing data
// (rows in the embeddings table) and, if found, requires the user to
// type the literal "yes" unless --yes was passed.
func confirmDestructiveImport(dbURL string, opts dbImportOptions) error {
	rowCount, err := countEmbeddings(dbURL)
	if err != nil {
		return fmt.Errorf("checking target database for existing data: %w", err)
	}
	if rowCount == 0 {
		return nil
	}

	host, dbName := describeTarget(dbURL)
	prompt := fmt.Sprintf(
		"Database %s at %s appears to contain data (%d rows in embeddings). "+
			"Importing will overwrite it. Type \"yes\" to continue: ",
		dbName, host, rowCount)

	if opts.yes {
		fmt.Println(prompt + "yes (auto-confirmed via --yes)")
		return nil
	}

	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(answer) != yesAnswerLiteral {
		return errors.New("import aborted by user")
	}
	return nil
}

// countEmbeddings returns the number of rows in the embeddings table.
// A missing table (fresh DB) is treated as zero rows.
func countEmbeddings(dbURL string) (int64, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var exists bool
	row := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		                WHERE table_schema='public' AND table_name='embeddings')`)
	if err := row.Scan(&exists); err != nil {
		return 0, fmt.Errorf("table check: %w", err)
	}
	if !exists {
		return 0, nil
	}

	var n int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return n, nil
}

// describeTarget extracts a host:port string and database name from a
// Postgres URL for display in the confirmation prompt.
func describeTarget(dbURL string) (host, dbName string) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return "<unknown host>", "<unknown db>"
	}
	host = u.Host
	if host == "" {
		host = "<unknown host>"
	}
	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = "<unknown db>"
	}
	return host, dbName
}

// dropPublicSchema wipes the public schema before a restore so a clean
// pg_restore / psql replay does not collide with pre-existing objects.
func dropPublicSchema(ctx context.Context, dbURL string) error {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	fmt.Println("Dropping public schema (--drop-existing) ...")
	if _, err := db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	return nil
}

// executeDBImport dispatches to the format-specific restore path.
func executeDBImport(ctx context.Context, dbURL string, opts dbImportOptions, format string, gzipped bool) error {
	switch format {
	case formatPlain:
		return importPlain(ctx, dbURL, opts.input, gzipped)
	case formatCustom:
		return importCustom(ctx, dbURL, opts.input, gzipped, opts.dropExisting)
	default:
		return fmt.Errorf("internal: unhandled dump format %q", format)
	}
}

// importPlain streams a plain SQL dump (optionally gunzipping it on the
// fly) into psql's stdin.
func importPlain(ctx context.Context, dbURL, inputPath string, gzipped bool) error {
	// #nosec G304 -- inputPath is the operator-supplied --input flag.
	f, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	var reader io.Reader = f
	if gzipped {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("gzip reader: %w", err)
		}
		defer gr.Close()
		reader = gr
	}

	fmt.Println("Running psql (plain SQL restore) ...")
	// #nosec G204 -- dbURL is operator-supplied via DATABASE_URL.
	cmd := exec.CommandContext(ctx, "psql",
		"--variable=ON_ERROR_STOP=1",
		"--dbname="+dbURL,
	)
	cmd.Stdin = reader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql failed: %w", err)
	}
	return nil
}

// importCustom invokes pg_restore against a custom-format dump. pg_restore
// requires a seekable file, so gzipped custom dumps are first
// decompressed to a temp file that is removed on the way out.
func importCustom(ctx context.Context, dbURL, inputPath string, gzipped, dropExisting bool) error {
	restorePath := inputPath
	if gzipped {
		tmp, err := decompressToTemp(inputPath)
		if err != nil {
			return fmt.Errorf("decompress custom dump: %w", err)
		}
		defer os.Remove(tmp)
		restorePath = tmp
	}

	args := []string{
		"--dbname=" + dbURL,
		"--no-owner",
		"--no-privileges",
	}
	if !dropExisting {
		// --clean drops each object before recreating it; --if-exists
		// suppresses errors for objects that are not yet present.
		args = append(args, "--clean", "--if-exists")
	}
	args = append(args, restorePath)

	fmt.Println("Running pg_restore (custom format) ...")
	// #nosec G204 -- dbURL/restorePath are operator-supplied.
	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore failed: %w", err)
	}
	return nil
}

// decompressToTemp writes the gunzipped contents of inputPath to a temp
// file (in the same directory as the input, so the rename-style atomicity
// is preserved across mounts) and returns the temp path. The copy is
// bounded by maxDecompressedDumpBytes to defuse a malicious gzip bomb.
func decompressToTemp(inputPath string) (string, error) {
	// #nosec G304 -- inputPath is the operator-supplied --input flag.
	in, err := os.Open(inputPath)
	if err != nil {
		return "", fmt.Errorf("open input: %w", err)
	}
	defer in.Close()

	gr, err := gzip.NewReader(in)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tmp, err := os.CreateTemp(filepath.Dir(inputPath), "photo-sorter-import-*.dump")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	// LimitReader + extra byte: if io.Copy reports exactly the limit, peek
	// one more byte to distinguish "exactly at limit" from "truncated by
	// limit". Anything past the limit fails loud.
	n, err := io.Copy(tmp, io.LimitReader(gr, maxDecompressedDumpBytes+1))
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("decompressing: %w", err)
	}
	if n > maxDecompressedDumpBytes {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("decompressed dump exceeds %d bytes — refusing as a safety guard",
			maxDecompressedDumpBytes)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp: %w", err)
	}
	return tmp.Name(), nil
}

// printImportNextSteps writes the post-import operator checklist. Keeping
// this in one place makes it trivial to keep in sync with the spec.
func printImportNextSteps() {
	fmt.Println()
	fmt.Println("Import complete.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Restart the photo-sorter server (HNSW indexes load at startup).")
	fmt.Println("  2. If the imported DB came from a host with a different originals tree,")
	fmt.Println("     verify STORAGE_ORIGINALS_PATH and re-run `photo-sorter cache build-thumbs`")
	fmt.Println("     to regenerate thumbnails for any missing sizes.")
	fmt.Println("  3. If face-search results look off, hit `POST /api/v1/process/rebuild-index`")
	fmt.Println("     (Rebuild Index button in the UI) to rebuild the in-memory HNSW indexes")
	fmt.Println("     from the freshly imported embeddings.")
}

// humanBytes formats a byte count using binary units (1024-based) with
// at most one decimal place — enough precision for a CLI summary line.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), suffix)
}
