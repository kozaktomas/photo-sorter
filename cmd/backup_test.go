package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

// writeOriginalsFixture creates a small originals tree under dir and returns
// the map of relative path -> file contents that callers can use as expected
// data for round-trip assertions.
func writeOriginalsFixture(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{
		"a.txt":                 "alpha",
		"2024/05/b.txt":         "bravo-content",
		"2024/06/c.bin":         "\x00\x01\x02charlie",
		"deep/nested/dir/d.txt": "delta-content-line",
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return files
}

// readTarBodies opens the archive at path and returns a map of
// Header.Name -> body bytes.
func readTarBodies(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	var reader io.Reader = f
	switch {
	case strings.HasSuffix(path, ".zst"):
		zr, zerr := zstd.NewReader(f)
		if zerr != nil {
			t.Fatalf("zstd reader: %v", zerr)
		}
		defer zr.Close()
		reader = zr
	case strings.HasSuffix(path, ".gz"):
		gz, gerr := gzip.NewReader(f)
		if gerr != nil {
			t.Fatalf("gzip reader: %v", gerr)
		}
		defer gz.Close()
		reader = gz
	}

	tr := tar.NewReader(reader)
	bodies := map[string]string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar Next: %v", err)
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar body for %s: %v", hdr.Name, err)
		}
		bodies[hdr.Name] = string(buf)
	}
	return bodies
}

// TestArchiveOriginals_roundTrip writes a small originals tree, archives it,
// and confirms every file round-trips through the tar+zstd pipeline byte for
// byte under the same relative paths.
func TestArchiveOriginals_roundTrip(t *testing.T) {
	t.Parallel()

	originals := t.TempDir()
	expected := writeOriginalsFixture(t, originals)
	out := filepath.Join(t.TempDir(), "originals.tar.zst")

	summary, err := archiveOriginals(context.Background(), originals, out, compressorZstd, 100)
	if err != nil {
		t.Fatalf("archiveOriginals: %v", err)
	}
	if summary.fileCount != len(expected) {
		t.Errorf("fileCount = %d, want %d", summary.fileCount, len(expected))
	}

	got := readTarBodies(t, out)
	if len(got) != len(expected) {
		t.Errorf("tar entry count = %d, want %d (entries: %v)", len(got), len(expected), got)
	}
	for rel, want := range expected {
		body, ok := got[rel]
		if !ok {
			t.Errorf("missing tar entry %q", rel)
			continue
		}
		if body != want {
			t.Errorf("tar body for %q = %q, want %q", rel, body, want)
		}
	}
}

// TestArchiveOriginals_gzip exercises the gzip path so the alternate
// compressor branch is covered.
func TestArchiveOriginals_gzip(t *testing.T) {
	t.Parallel()

	originals := t.TempDir()
	expected := writeOriginalsFixture(t, originals)
	out := filepath.Join(t.TempDir(), "originals.tar.gz")

	if _, err := archiveOriginals(context.Background(), originals, out, compressorGzip, 100); err != nil {
		t.Fatalf("archiveOriginals gzip: %v", err)
	}
	got := readTarBodies(t, out)
	if len(got) != len(expected) {
		t.Errorf("gzip tar entry count = %d, want %d", len(got), len(expected))
	}
}

// TestArchiveOriginals_followsSymlinks ensures a regular-file symlink target
// is included in the archive under the symlink's relative path.
func TestArchiveOriginals_followsSymlinks(t *testing.T) {
	t.Parallel()

	originals := t.TempDir()
	target := filepath.Join(originals, "real.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(originals, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported on this filesystem: %v", err)
	}

	out := filepath.Join(t.TempDir(), "originals.tar.zst")
	summary, err := archiveOriginals(context.Background(), originals, out, compressorZstd, 100)
	if err != nil {
		t.Fatalf("archiveOriginals: %v", err)
	}
	if summary.fileCount != 2 {
		t.Errorf("fileCount = %d, want 2", summary.fileCount)
	}
	got := readTarBodies(t, out)
	if got["link.txt"] != "payload" {
		t.Errorf("symlinked entry = %q, want payload", got["link.txt"])
	}
}

// TestExecuteBackup_skipDB runs the full backup orchestration with --skip-db
// and confirms metadata.json + the tar archive end up inside a renamed
// final directory.
func TestExecuteBackup_skipDB(t *testing.T) {
	t.Parallel()

	originals := t.TempDir()
	expected := writeOriginalsFixture(t, originals)
	output := t.TempDir()

	opts := backupOptions{
		output:        output,
		originalsPath: originals,
		keep:          0,
		compressor:    compressorZstd,
		skipDB:        true,
		progressEvery: 50,
	}

	if err := executeBackup(context.Background(), opts); err != nil {
		t.Fatalf("executeBackup: %v", err)
	}

	dirs := listFinishedBackups(t, output)
	if len(dirs) != 1 {
		t.Fatalf("expected 1 finished backup, got %d (%v)", len(dirs), dirs)
	}
	finalDir := filepath.Join(output, dirs[0])

	metaBytes, err := os.ReadFile(filepath.Join(finalDir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta backupResult
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.FileCount != len(expected) {
		t.Errorf("metadata.FileCount = %d, want %d", meta.FileCount, len(expected))
	}
	if meta.DBSizeBytes != 0 {
		t.Errorf("metadata.DBSizeBytes = %d, want 0 (--skip-db)", meta.DBSizeBytes)
	}

	archive := filepath.Join(finalDir, "originals.tar.zst")
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("expected tar archive at %s: %v", archive, err)
	}
}

// listFinishedBackups returns the photo-sorter-* subdirectories of output,
// sorted ascending, for use in retention assertions.
func listFinishedBackups(t *testing.T, output string) []string {
	t.Helper()
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), backupDirPrefix) {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// TestPruneBackups_keepsLatestN seeds five fake finished-backup directories
// with monotonically increasing timestamps and confirms that pruneBackups
// retains exactly N of them, deleting the oldest first.
func TestPruneBackups_keepsLatestN(t *testing.T) {
	t.Parallel()

	output := t.TempDir()
	timestamps := []string{
		"20240101-000000",
		"20240102-000000",
		"20240103-000000",
		"20240104-000000",
		"20240105-000000",
	}
	for _, ts := range timestamps {
		path := finalDirPath(output, ts)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	pruned, err := pruneBackups(output, 2)
	if err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}
	if pruned != 3 {
		t.Errorf("pruned = %d, want 3", pruned)
	}

	remaining := listFinishedBackups(t, output)
	wantRemaining := []string{
		backupDirPrefix + "20240104-000000",
		backupDirPrefix + "20240105-000000",
	}
	if !equalStringSlices(remaining, wantRemaining) {
		t.Errorf("remaining = %v, want %v", remaining, wantRemaining)
	}
}

// TestPruneBackups_keepZeroDisablesPruning asserts that keep<=0 leaves the
// output directory untouched.
func TestPruneBackups_keepZeroDisablesPruning(t *testing.T) {
	t.Parallel()

	output := t.TempDir()
	for _, ts := range []string{"20240101-000000", "20240102-000000"} {
		if err := os.MkdirAll(finalDirPath(output, ts), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	pruned, err := pruneBackups(output, 0)
	if err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}
	if pruned != 0 {
		t.Errorf("pruned with keep=0 = %d, want 0", pruned)
	}
	if got := listFinishedBackups(t, output); len(got) != 2 {
		t.Errorf("remaining = %d, want 2", len(got))
	}
}

// TestPruneBackups_ignoresTmpDirs makes sure the in-flight .photo-sorter-*.tmp
// directories are not counted toward retention and are never removed by it.
func TestPruneBackups_ignoresTmpDirs(t *testing.T) {
	t.Parallel()

	output := t.TempDir()
	if err := os.MkdirAll(finalDirPath(output, "20240105-000000"), 0o755); err != nil {
		t.Fatalf("mkdir final: %v", err)
	}
	tmpDir := tmpDirPath(output, "20240106-000000")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if _, err := pruneBackups(output, 1); err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}
	if _, err := os.Stat(tmpDir); err != nil {
		t.Errorf("tmp dir should be left alone, got: %v", err)
	}
}

// equalStringSlices compares two string slices for ordered equality.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPgDump_subprocess exercises the real pg_dump subprocess against a
// disposable testcontainers Postgres. Skipped if either pg_dump or Docker is
// unavailable.
func TestPgDump_subprocess(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not in PATH; skipping subprocess test")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH; skipping subprocess test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dsn, terminate := startPostgresContainer(ctx, t)
	defer terminate()

	seedTestDatabase(ctx, t, dsn)

	out := filepath.Join(t.TempDir(), "db.sql.zst")
	size, err := dumpDatabase(ctx, dsn, out, compressorZstd)
	if err != nil {
		t.Fatalf("dumpDatabase: %v", err)
	}
	if size <= 0 {
		t.Errorf("dump size = %d, want > 0", size)
	}

	sqlText := decompressZstd(t, out)
	if !strings.Contains(sqlText, "backup_marker") {
		t.Errorf("dump does not mention seeded table; got prefix %q",
			truncateString(sqlText, 200))
	}
}

// startPostgresContainer spins up a throwaway Postgres container and returns
// its connection URL plus a cleanup function. Fails the test if the
// container or its readiness probe cannot be satisfied.
func startPostgresContainer(ctx context.Context, t *testing.T) (string, func()) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:17-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "tester",
			"POSTGRES_PASSWORD": "tester",
			"POSTGRES_DB":       "backup_test",
		},
		WaitingFor: tcwait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2 * time.Minute),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("could not start postgres container: %v", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("tester", "tester"),
		Host:     net.JoinHostPort(host, port.Port()),
		Path:     "/backup_test",
		RawQuery: "sslmode=disable",
	}
	cleanup := func() {
		_ = container.Terminate(context.Background())
	}
	return u.String(), cleanup
}

// seedTestDatabase opens a connection and creates a single recognisable table
// so the dump output can be searched for a known string.
func seedTestDatabase(ctx context.Context, t *testing.T, dsn string) {
	t.Helper()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE TABLE backup_marker (id int)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

// decompressZstd reads a zstd-compressed file and returns its plain contents.
func decompressZstd(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open dump: %v", err)
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer zr.Close()
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

// truncateString returns s clipped to at most n runes, useful for error
// messages that should not flood the test log.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
