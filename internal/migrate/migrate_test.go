//go:build integration

package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testFixture bundles every handle a migration test needs.
type testFixture struct {
	maria     *sql.DB
	pgPool    *postgres.Pool
	store     *storage.Storage
	root      string
	cleanup   func()
	subjects  *postgres.SubjectRepository
	labels    *postgres.LabelRepository
	albums    *postgres.AlbumRepository
	markers   *postgres.MarkerRepository
	photos    *postgres.PhotoRepository
	originals string
	cachePath string
}

// setupFixture brings up a MariaDB + pgvector pair, seeds the source
// schema with a minimal subset of PhotoPrism tables, drops 3 sample JPEGs
// into the originals tree, and returns the wired fixture.
func setupFixture(t *testing.T) *testFixture {
	t.Helper()
	ctx := context.Background()

	pgPool, pgCleanup := startPostgres(t, ctx)
	if pgPool == nil {
		return nil
	}
	mariaDB, mariaCleanup := startMariaDB(t, ctx)
	if mariaDB == nil {
		pgCleanup()
		return nil
	}

	if err := seedPhotoPrismSchema(ctx, mariaDB); err != nil {
		pgCleanup()
		mariaCleanup()
		t.Fatalf("seed mariadb: %v", err)
	}

	root := t.TempDir()
	originals := filepath.Join(root, "src-originals")
	cachePath := filepath.Join(root, "cache")
	dest := filepath.Join(root, "dest-originals")
	if err := os.MkdirAll(originals, 0o755); err != nil {
		t.Fatalf("mkdir originals: %v", err)
	}
	store, err := storage.New(dest, cachePath)
	if err != nil {
		pgCleanup()
		mariaCleanup()
		t.Fatalf("storage.New: %v", err)
	}

	if err := seedPhotoPrismRows(ctx, mariaDB, originals); err != nil {
		pgCleanup()
		mariaCleanup()
		t.Fatalf("seed rows: %v", err)
	}

	cleanup := func() {
		pgCleanup()
		mariaCleanup()
	}
	return &testFixture{
		maria:     mariaDB,
		pgPool:    pgPool,
		store:     store,
		root:      root,
		cleanup:   cleanup,
		subjects:  postgres.NewSubjectRepository(pgPool),
		labels:    postgres.NewLabelRepository(pgPool),
		albums:    postgres.NewAlbumRepository(pgPool),
		markers:   postgres.NewMarkerRepository(pgPool),
		photos:    postgres.NewPhotoRepository(pgPool),
		originals: originals,
		cachePath: cachePath,
	}
}

// startPostgres launches a pgvector container, applies the photo-sorter
// migrations, and returns the pool + a cleanup. Tests that cannot reach
// Docker are skipped.
func startPostgres(t *testing.T, ctx context.Context) (*postgres.Pool, func()) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(90 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil || container == nil {
		t.Skipf("Docker not available, skipping integration test: %v", err)
		return nil, func() {}
	}
	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "5432")
	dsn := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())
	pool, err := postgres.NewPool(&config.DatabaseConfig{
		URL:          dsn,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	})
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("pg pool: %v", err)
	}
	if err := pool.Migrate(ctx); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("pg migrate: %v", err)
	}
	cleanup := func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
	return pool, cleanup
}

// startMariaDB launches a MariaDB container with empty database
// "photoprism" and returns the opened *sql.DB + cleanup.
func startMariaDB(t *testing.T, ctx context.Context) (*sql.DB, func()) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "mariadb:10.11",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MARIADB_ROOT_PASSWORD": "root",
			"MARIADB_DATABASE":      "photoprism",
			"MARIADB_USER":          "photoprism",
			"MARIADB_PASSWORD":      "photoprism",
		},
		WaitingFor: wait.ForLog("port: 3306  mariadb.org binary distribution").
			WithStartupTimeout(120 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil || container == nil {
		t.Skipf("Docker not available, skipping integration test: %v", err)
		return nil, func() {}
	}
	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "3306")
	dsn := fmt.Sprintf("photoprism:photoprism@tcp(%s:%s)/photoprism?parseTime=true", host, port.Port())

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("open mariadb: %v", err)
	}
	// Server may report ready before accepting client connections; retry
	// a handful of times before giving up.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err = db.PingContext(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		_ = db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("ping mariadb: %v", err)
	}
	cleanup := func() {
		_ = db.Close()
		_ = container.Terminate(ctx)
	}
	return db, cleanup
}

// seedPhotoPrismSchema creates the subset of PhotoPrism tables the
// migrator reads. Only columns the migrator actually consults are
// included; everything else is omitted to keep the test self-contained.
func seedPhotoPrismSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE photos (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			photo_uid VARBINARY(42) NOT NULL UNIQUE,
			taken_at DATETIME NULL,
			taken_at_local DATETIME NULL,
			photo_title VARCHAR(200) NULL,
			photo_caption VARCHAR(4096) NULL,
			photo_lat DOUBLE NULL,
			photo_lng DOUBLE NULL,
			photo_altitude INT NULL,
			photo_iso INT NULL,
			photo_f_number FLOAT NULL,
			photo_exposure VARBINARY(64) NULL,
			photo_focal_length INT NULL,
			photo_favorite TINYINT NULL,
			photo_private TINYINT NULL,
			photo_panorama TINYINT NULL,
			photo_scan TINYINT NULL,
			photo_quality SMALLINT NULL,
			time_zone VARBINARY(64) NULL,
			camera_id INT NULL,
			lens_id INT NULL,
			deleted_at DATETIME NULL
		)`,
		`CREATE TABLE files (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			file_uid VARBINARY(42) NOT NULL UNIQUE,
			photo_uid VARBINARY(42) NULL,
			file_name VARBINARY(1024) NULL,
			file_size BIGINT NULL,
			file_mime VARBINARY(64) NULL,
			file_width INT NULL,
			file_height INT NULL,
			file_orientation INT NULL,
			file_primary TINYINT NULL,
			file_sidecar TINYINT NULL,
			file_missing TINYINT NULL,
			deleted_at DATETIME NULL
		)`,
		`CREATE TABLE cameras (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			camera_make VARCHAR(160) NULL,
			camera_model VARCHAR(160) NULL
		)`,
		`CREATE TABLE lenses (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			lens_model VARCHAR(160) NULL
		)`,
		`CREATE TABLE details (
			photo_id INT NOT NULL PRIMARY KEY,
			notes VARCHAR(2048) NULL,
			keywords VARCHAR(2048) NULL,
			artist VARCHAR(1024) NULL,
			copyright VARCHAR(1024) NULL,
			license VARCHAR(1024) NULL,
			software VARCHAR(1024) NULL
		)`,
		`CREATE TABLE keywords (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			keyword VARCHAR(64) NULL,
			skip TINYINT NULL
		)`,
		`CREATE TABLE photos_keywords (
			photo_id INT NOT NULL,
			keyword_id INT NOT NULL,
			PRIMARY KEY (photo_id, keyword_id)
		)`,
		`CREATE TABLE subjects (
			subj_uid VARBINARY(42) NOT NULL PRIMARY KEY,
			subj_name VARCHAR(160) NULL,
			subj_type VARBINARY(8) NULL,
			subj_favorite TINYINT NULL,
			subj_private TINYINT NULL,
			deleted_at DATETIME NULL
		)`,
		`CREATE TABLE labels (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			label_name VARCHAR(160) NULL,
			label_slug VARBINARY(160) NULL,
			label_priority INT NULL,
			label_favorite TINYINT NULL,
			deleted_at DATETIME NULL
		)`,
		`CREATE TABLE photos_labels (
			photo_id INT NOT NULL,
			label_id INT NOT NULL,
			label_src VARBINARY(8) NULL,
			uncertainty SMALLINT NULL,
			PRIMARY KEY (photo_id, label_id)
		)`,
		`CREATE TABLE albums (
			id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			album_uid VARBINARY(42) NOT NULL UNIQUE,
			album_slug VARBINARY(160) NULL,
			album_title VARCHAR(160) NULL,
			album_description VARCHAR(2048) NULL,
			album_type VARBINARY(8) NULL,
			album_favorite TINYINT NULL,
			album_private TINYINT NULL,
			deleted_at DATETIME NULL
		)`,
		`CREATE TABLE photos_albums (
			photo_uid VARBINARY(42) NOT NULL,
			album_uid VARBINARY(42) NOT NULL,
			hidden TINYINT NULL,
			missing TINYINT NULL,
			PRIMARY KEY (photo_uid, album_uid)
		)`,
		`CREATE TABLE markers (
			marker_uid VARBINARY(42) NOT NULL PRIMARY KEY,
			file_uid VARBINARY(42) NULL,
			subj_uid VARBINARY(42) NULL,
			marker_type VARBINARY(8) NULL,
			x FLOAT NULL,
			y FLOAT NULL,
			w FLOAT NULL,
			h FLOAT NULL,
			score SMALLINT NULL,
			marker_invalid TINYINT NULL,
			marker_review TINYINT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("exec %q: %w", s, err)
		}
	}
	return nil
}

// seedPhotoPrismRows inserts a small fixture: 3 photos with primary
// files, 2 labels, 1 album, 1 subject, 1 marker. Sample JPEGs are
// written to the originals dir at YYYY/MM/<name>.jpg so the migrator's
// path resolution matches the production layout.
func seedPhotoPrismRows(ctx context.Context, db *sql.DB, originalsRoot string) error {
	if err := writeSampleJPEGs(originalsRoot); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO cameras (id, camera_make, camera_model) VALUES
			(1, 'Canon', 'EOS R5')`); err != nil {
		return fmt.Errorf("insert camera: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO lenses (id, lens_model) VALUES (1, 'RF 24-70mm F2.8')`); err != nil {
		return fmt.Errorf("insert lens: %w", err)
	}
	if err := insertSubjectsLabelsAlbums(ctx, db); err != nil {
		return err
	}
	if err := insertPhotosFilesDetails(ctx, db); err != nil {
		return err
	}
	if err := insertMembershipsAndMarkers(ctx, db); err != nil {
		return err
	}
	return nil
}

// insertSubjectsLabelsAlbums seeds the lookup tables.
func insertSubjectsLabelsAlbums(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO subjects (subj_uid, subj_name, subj_type, subj_favorite, subj_private)
		 VALUES ('s001', 'Alice Example', 'person', 1, 0)`); err != nil {
		return fmt.Errorf("insert subject: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO labels (id, label_name, label_slug, label_priority, label_favorite) VALUES
			(1, 'Nature', 'nature', 5, 1),
			(2, 'Sunset', 'sunset', 0, 0)`); err != nil {
		return fmt.Errorf("insert label: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO albums (id, album_uid, album_slug, album_title, album_description,
		                     album_type, album_favorite, album_private)
		 VALUES (1, 'a001', 'holiday-2024', 'Holiday 2024', 'Trip notes',
		         'album', 0, 0)`); err != nil {
		return fmt.Errorf("insert album: %w", err)
	}
	return nil
}

// insertPhotosFilesDetails inserts the three photo rows + their primary
// files + the details table notes for photo #1. Photo #1 also exercises
// the gap-fix metadata columns (panorama, scan, quality, time_zone,
// taken_at_local for offset, keywords + EXIF-ish fields) so the
// end-to-end test can assert they round-trip into the destination.
func insertPhotosFilesDetails(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO photos (id, photo_uid, taken_at, taken_at_local,
		                     photo_title, photo_caption,
		                     photo_lat, photo_lng, photo_iso, photo_f_number, photo_exposure,
		                     photo_focal_length, photo_favorite, photo_private,
		                     photo_panorama, photo_scan, photo_quality, time_zone,
		                     camera_id, lens_id)
		 VALUES
			(1, 'p001', '2024-06-15 10:00:00', '2024-06-15 12:00:00',
			 'Sunset', 'A nice sunset',
			 49.5, 16.5, 100, 2.8, '1/200', 35, 1, 0,
			 1, 0, 5, 'Europe/Prague',
			 1, 1),
			(2, 'p002', '2024-07-04 09:30:00', '2024-07-04 09:30:00',
			 'Park walk', '',
			 NULL, NULL, NULL, NULL, '', NULL, 0, 0,
			 0, 1, 3, 'Local',
			 1, NULL),
			(3, 'p003', NULL, NULL, 'No date', '',
			 NULL, NULL, NULL, NULL, '', NULL, 0, 1,
			 0, 0, 0, NULL,
			 NULL, NULL)`,
	); err != nil {
		return fmt.Errorf("insert photos: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO files (id, file_uid, photo_uid, file_name, file_size, file_mime,
		                    file_width, file_height, file_orientation,
		                    file_primary, file_sidecar, file_missing)
		 VALUES
			(1, 'f001', 'p001', '2024/06/IMG_0001.jpg', 1000, 'image/jpeg', 32, 24, 1, 1, 0, 0),
			(2, 'f002', 'p002', '2024/07/IMG_0002.jpg', 1000, 'image/jpeg', 32, 24, 1, 1, 0, 0),
			(3, 'f003', 'p003', 'unknown/IMG_0003.jpg', 1000, 'image/jpeg', 32, 24, 1, 1, 0, 0)`,
	); err != nil {
		return fmt.Errorf("insert files: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO details (photo_id, notes, keywords, artist, copyright, license, software)
		 VALUES
			(1, 'shot at golden hour', 'sunset, golden hour, Veselice, 🌅',
			 'Alice Photographer', '(c) 2024 Alice', 'CC BY-SA 4.0', 'PhotoPrism 240801')`); err != nil {
		return fmt.Errorf("insert details: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO keywords (id, keyword, skip) VALUES
			(1, 'sunset', 0),
			(2, 'czech republic', 0),
			(3, 'auto-ignored', 1)`); err != nil {
		return fmt.Errorf("insert keywords: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO photos_keywords (photo_id, keyword_id) VALUES
			(1, 1),
			(1, 2),
			(1, 3)`); err != nil {
		return fmt.Errorf("insert photos_keywords: %w", err)
	}
	return nil
}

// insertMembershipsAndMarkers attaches labels, album memberships, and a
// face marker to the seeded photos.
func insertMembershipsAndMarkers(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO photos_labels (photo_id, label_id, label_src, uncertainty) VALUES
			(1, 1, 'manual', 0),
			(1, 2, 'manual', 0),
			(2, 1, 'manual', 0)`,
	); err != nil {
		return fmt.Errorf("insert photos_labels: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO photos_albums (photo_uid, album_uid, hidden, missing) VALUES
			('p001', 'a001', 0, 0),
			('p002', 'a001', 0, 0)`); err != nil {
		return fmt.Errorf("insert photos_albums: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO markers (marker_uid, file_uid, subj_uid, marker_type,
		                       x, y, w, h, score, marker_invalid, marker_review)
		 VALUES ('m001', 'f001', 's001', 'face',
		         0.25, 0.30, 0.20, 0.25, 80, 0, 1)`); err != nil {
		return fmt.Errorf("insert markers: %w", err)
	}
	return nil
}

// writeSampleJPEGs encodes three tiny JPEGs at the YYYY/MM/* paths the
// PhotoPrism fixture refers to. Each file is unique so SHA256 hashes
// differ.
func writeSampleJPEGs(root string) error {
	files := map[string]byte{
		"2024/06/IMG_0001.jpg": 0x01,
		"2024/07/IMG_0002.jpg": 0x02,
		"unknown/IMG_0003.jpg": 0x03,
	}
	for rel, seed := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", full, err)
		}
		data := tinyJPEG(seed)
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}
	return nil
}

// tinyJPEG renders a deterministic 32×24 JPEG painted with the supplied
// seed so each fixture file has a distinct SHA256.
func tinyJPEG(seed byte) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x*8) ^ seed,
				G: uint8(y*16) ^ seed,
				B: seed,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

// TestMigrationEndToEnd exercises the migrator against real MariaDB +
// pgvector containers. Photos, labels, albums, subjects, markers are
// migrated; the test asserts the row counts and the originals tree.
func TestMigrationEndToEnd(t *testing.T) {
	fx := setupFixture(t)
	if fx == nil {
		return
	}
	defer fx.cleanup()

	ctx := context.Background()
	opts := buildOptions(fx)
	report, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("first migration: %v", err)
	}
	assertFirstRunCounts(t, report)
	assertFirstRunPersisted(t, ctx, fx)
	assertPhotoUIDsPreserved(t, ctx, fx)
	assertGapFixFieldsPersisted(t, ctx, fx)
	assertPreExistingReferencesReconnect(t, ctx, fx)

	// Re-run: every stage should now report zero creations because the
	// destination already has the rows.
	report2, err := Run(ctx, buildOptions(fx))
	if err != nil {
		t.Fatalf("re-run migration: %v", err)
	}
	assertReRunNoNewRows(t, report2)
	assertGapFixBackfillPreservesEdits(t, ctx, fx)
}

// assertGapFixFieldsPersisted checks the metadata fields added by task
// 68fc8ca2 — keywords, panorama, scan, quality, time_zone,
// taken_at_offset, and the EXIF-ish artist / copyright / license /
// software fields. Photo p001 in the fixture exercises non-default
// values for all of them so this assertion covers first-run writes.
func assertGapFixFieldsPersisted(t *testing.T, ctx context.Context, fx *testFixture) {
	t.Helper()
	photo, err := fx.photos.GetPhoto(ctx, "p001")
	if err != nil {
		t.Fatalf("get p001: %v", err)
	}
	// Keywords: details.keywords ("sunset, golden hour, Veselice, 🌅")
	// + photos_keywords ("sunset", "czech republic"; "auto-ignored" is
	// skip=1 and must be filtered out). Order is "details first then
	// join", with sunset deduped.
	wantKW := map[string]struct{}{
		"sunset":      {},
		"golden hour": {},
		"Veselice":    {},
		"🌅":           {},
		// from photos_keywords (skip=0 only; "auto-ignored" stays out):
		"czech republic": {},
	}
	if len(photo.Keywords) != len(wantKW) {
		t.Errorf("keywords = %v, want %d items", photo.Keywords, len(wantKW))
	}
	for _, kw := range photo.Keywords {
		if _, ok := wantKW[kw]; !ok {
			t.Errorf("unexpected keyword %q in %v", kw, photo.Keywords)
		}
	}
	if !photo.Panorama {
		t.Errorf("p001 panorama = false, want true")
	}
	if photo.Scan {
		t.Errorf("p001 scan = true, want false")
	}
	if photo.Quality != 5 {
		t.Errorf("p001 quality = %d, want 5", photo.Quality)
	}
	if photo.TimeZone != "Europe/Prague" {
		t.Errorf("p001 time_zone = %q, want Europe/Prague", photo.TimeZone)
	}
	if photo.TakenAtOffset != 2*60*60 {
		t.Errorf("p001 taken_at_offset = %d seconds, want %d (2h)",
			photo.TakenAtOffset, 2*60*60)
	}
	if photo.ExifArtist != "Alice Photographer" {
		t.Errorf("p001 exif_artist = %q", photo.ExifArtist)
	}
	if photo.ExifCopyright != "(c) 2024 Alice" {
		t.Errorf("p001 exif_copyright = %q", photo.ExifCopyright)
	}
	if photo.ExifLicense != "CC BY-SA 4.0" {
		t.Errorf("p001 exif_license = %q", photo.ExifLicense)
	}
	if photo.ExifSoftware != "PhotoPrism 240801" {
		t.Errorf("p001 exif_software = %q", photo.ExifSoftware)
	}

	// p002 carries time_zone "Local" — the migrator normalises that
	// sentinel to empty string. Its taken_at == taken_at_local so the
	// computed offset is 0.
	p2, err := fx.photos.GetPhoto(ctx, "p002")
	if err != nil {
		t.Fatalf("get p002: %v", err)
	}
	if p2.TimeZone != "" {
		t.Errorf(`p002 time_zone = %q, want "" (Local sentinel)`, p2.TimeZone)
	}
	if p2.TakenAtOffset != 0 {
		t.Errorf("p002 taken_at_offset = %d, want 0", p2.TakenAtOffset)
	}
	if !p2.Scan {
		t.Errorf("p002 scan = false, want true")
	}
	if p2.Quality != 3 {
		t.Errorf("p002 quality = %d, want 3", p2.Quality)
	}
	if len(p2.Keywords) != 0 {
		t.Errorf("p002 keywords = %v, want empty", p2.Keywords)
	}

	// p003 has no details row and a NULL time_zone — every gap-fix
	// field must arrive at the column's zero value.
	p3, err := fx.photos.GetPhoto(ctx, "p003")
	if err != nil {
		t.Fatalf("get p003: %v", err)
	}
	if p3.TimeZone != "" || p3.TakenAtOffset != 0 || p3.Quality != 0 {
		t.Errorf("p003 unexpected non-zero: tz=%q offset=%d quality=%d",
			p3.TimeZone, p3.TakenAtOffset, p3.Quality)
	}
	if p3.ExifArtist != "" || len(p3.Keywords) != 0 {
		t.Errorf("p003 unexpected non-zero: artist=%q keywords=%v",
			p3.ExifArtist, p3.Keywords)
	}
}

// assertGapFixBackfillPreservesEdits verifies the merge semantics of the
// re-run path: the migrator backfills fields whose destination value is
// still the column zero value but never overwrites a value the user has
// already edited natively. Setup: between the first and second run the
// test mutates p001 to mimic a manual edit, then we assert the second
// run leaves the edited value alone while still filling p003's defaults
// once the operator goes back and tags it in PhotoPrism.
func assertGapFixBackfillPreservesEdits(t *testing.T, ctx context.Context, fx *testFixture) {
	t.Helper()
	// Step 1: a user edits p001 in the native UI, changing the artist
	// string and adding a private keyword the PhotoPrism source does
	// not know about. The migrator must NOT clobber those.
	edit, err := fx.photos.GetPhoto(ctx, "p001")
	if err != nil {
		t.Fatalf("get p001: %v", err)
	}
	edit.ExifArtist = "Edited by native UI"
	edit.Keywords = []string{"native-only-tag"}
	if err := fx.photos.UpdatePhoto(ctx, edit); err != nil {
		t.Fatalf("manual edit p001: %v", err)
	}

	// Step 2: a third migration run executes; backfill must see "Edit"
	// in ExifArtist (non-default) and leave it alone.
	if _, err := Run(ctx, buildOptions(fx)); err != nil {
		t.Fatalf("third migration: %v", err)
	}
	after, err := fx.photos.GetPhoto(ctx, "p001")
	if err != nil {
		t.Fatalf("re-get p001: %v", err)
	}
	if after.ExifArtist != "Edited by native UI" {
		t.Errorf("ExifArtist after re-run = %q, user edit was clobbered",
			after.ExifArtist)
	}
	if len(after.Keywords) != 1 || after.Keywords[0] != "native-only-tag" {
		t.Errorf("Keywords after re-run = %v, want [native-only-tag]",
			after.Keywords)
	}

	// Step 3: simulate p003 (which has no metadata in the source yet)
	// being tagged in PhotoPrism. After updating the source row, a
	// re-run must backfill the columns that are still default.
	if _, err := fx.maria.ExecContext(ctx,
		`INSERT INTO details (photo_id, notes, keywords, artist)
		 VALUES (3, 'late add', 'birds,trees', 'New artist')`); err != nil {
		t.Fatalf("seed details for p003: %v", err)
	}
	if _, err := fx.maria.ExecContext(ctx,
		`UPDATE photos SET photo_quality = 4, time_zone = 'UTC' WHERE id = 3`); err != nil {
		t.Fatalf("update p003 in mariadb: %v", err)
	}
	if _, err := Run(ctx, buildOptions(fx)); err != nil {
		t.Fatalf("fourth migration: %v", err)
	}
	p3, err := fx.photos.GetPhoto(ctx, "p003")
	if err != nil {
		t.Fatalf("get p003 after backfill: %v", err)
	}
	if p3.Quality != 4 {
		t.Errorf("p003 quality after backfill = %d, want 4", p3.Quality)
	}
	if p3.TimeZone != "UTC" {
		t.Errorf("p003 time_zone after backfill = %q, want UTC", p3.TimeZone)
	}
	if p3.ExifArtist != "New artist" {
		t.Errorf("p003 exif_artist after backfill = %q", p3.ExifArtist)
	}
	wantKW3 := map[string]struct{}{"birds": {}, "trees": {}}
	if len(p3.Keywords) != len(wantKW3) {
		t.Errorf("p003 keywords after backfill = %v, want 2 items", p3.Keywords)
	}
	for _, kw := range p3.Keywords {
		if _, ok := wantKW3[kw]; !ok {
			t.Errorf("p003 unexpected keyword %q", kw)
		}
	}
}

// assertPhotoUIDsPreserved checks that every native photo row carries
// the PhotoPrism photo_uid as its primary key (the bug this task fixed).
// Subjects, albums, and markers get the same check — preserving those
// UIDs is what keeps faces.subject_uid / faces.marker_uid pointing at
// the right rows.
func assertPhotoUIDsPreserved(t *testing.T, ctx context.Context, fx *testFixture) {
	t.Helper()
	wantPhotoUIDs := map[string]bool{"p001": false, "p002": false, "p003": false}
	rows, err := fx.pgPool.Query(ctx, `SELECT uid FROM photos`)
	if err != nil {
		t.Fatalf("select photo uids: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatalf("scan photo uid: %v", err)
		}
		if _, ok := wantPhotoUIDs[uid]; !ok {
			t.Errorf("photos.uid = %q, not a PhotoPrism UID we migrated", uid)
			continue
		}
		wantPhotoUIDs[uid] = true
	}
	for uid, seen := range wantPhotoUIDs {
		if !seen {
			t.Errorf("photos.uid = %q missing — not preserved from PhotoPrism", uid)
		}
	}

	var subjectUID string
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT uid FROM subjects WHERE name = 'Alice Example'`).Scan(&subjectUID); err != nil {
		t.Fatalf("select subject: %v", err)
	}
	if subjectUID != "s001" {
		t.Errorf("subject Alice Example: uid = %q, want %q", subjectUID, "s001")
	}

	var albumUID string
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT uid FROM albums WHERE slug = 'holiday-2024'`).Scan(&albumUID); err != nil {
		t.Fatalf("select album: %v", err)
	}
	if albumUID != "a001" {
		t.Errorf("album holiday-2024: uid = %q, want %q", albumUID, "a001")
	}

	var markerUID string
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT uid FROM markers LIMIT 1`).Scan(&markerUID); err != nil {
		t.Fatalf("select marker: %v", err)
	}
	if markerUID != "m001" {
		t.Errorf("marker uid = %q, want %q", markerUID, "m001")
	}
}

// assertPreExistingReferencesReconnect simulates the real motivation for
// the photo_uid preservation: any embedding / section_photos row that
// was created against a PhotoPrism photo UID *before* the migration ran
// should still find its photo afterwards. We can't run this BEFORE the
// migration because the photos rows don't exist yet — so we insert the
// reference rows here (post-migration) and confirm the JOIN counts
// match.
func assertPreExistingReferencesReconnect(
	t *testing.T, ctx context.Context, fx *testFixture,
) {
	t.Helper()
	// Use a 768-element vector literal — pgvector accepts any sized
	// VECTOR(768) value as a string in the form '[0,0,...]'.
	embedding := embeddingZeroLiteral(768)
	if _, err := fx.pgPool.Exec(ctx,
		`INSERT INTO embeddings (photo_uid, embedding, model, pretrained)
		 VALUES ('p001', $1::vector, 'test', 'test')`,
		embedding,
	); err != nil {
		t.Fatalf("seed embedding by PhotoPrism uid: %v", err)
	}
	var joined int
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM embeddings e
		 JOIN photos p ON p.uid = e.photo_uid
		 WHERE e.photo_uid = 'p001'`).Scan(&joined); err != nil {
		t.Fatalf("count joined embeddings: %v", err)
	}
	if joined != 1 {
		t.Errorf("embeddings → photos join on PhotoPrism UID: count = %d, want 1", joined)
	}
}

// embeddingZeroLiteral returns a pgvector-friendly literal "[0,0,...]"
// of the requested dimension. Vectors are stored as text on the wire
// before pgvector parses them, so a string literal is the easiest way to
// seed a row without an extra Go dependency.
func embeddingZeroLiteral(dim int) string {
	parts := make([]byte, 0, dim*2+2)
	parts = append(parts, '[')
	for i := 0; i < dim; i++ {
		if i > 0 {
			parts = append(parts, ',')
		}
		parts = append(parts, '0')
	}
	parts = append(parts, ']')
	return string(parts)
}

// buildOptions assembles a fresh Options bound to the test fixture. A
// new bytes.Buffer for the writer keeps stdout clean.
func buildOptions(fx *testFixture) *Options {
	return &Options{
		MariaDB:       fx.maria,
		OriginalsRoot: fx.originals,
		CacheRoot:     fx.cachePath,
		UploaderUID:   "",
		DryRun:        false,
		SkipThumbs:    true, // thumbs covered by its own test path; not part of this assertion set
		BatchSize:     200,
		Concurrency:   2,
		Store:         fx.store,
		Photos:        fx.photos,
		Subjects:      fx.subjects,
		Labels:        fx.labels,
		Albums:        fx.albums,
		Markers:       fx.markers,
		Writer:        &bytes.Buffer{},
	}
}

// assertFirstRunCounts checks that the run inserted the expected number
// of rows in every stage.
func assertFirstRunCounts(t *testing.T, report *Report) {
	t.Helper()
	want := map[string]int{
		StageSubjects:  1,
		StagePhotos:    3,
		StageLabels:    2,
		"photo_labels": 3,
		StageAlbums:    1,
		"album_photos": 2,
		StageMarkers:   1,
	}
	for _, s := range report.Stages {
		if expected, ok := want[s.Stage]; ok {
			if s.Created != expected {
				t.Errorf("stage %s: Created = %d, want %d", s.Stage, s.Created, expected)
			}
			delete(want, s.Stage)
		}
	}
	for stage := range want {
		t.Errorf("stage %s missing from report", stage)
	}
}

// assertFirstRunPersisted reads the destination Postgres and on-disk
// originals and verifies the migration's side effects.
func assertFirstRunPersisted(t *testing.T, ctx context.Context, fx *testFixture) {
	t.Helper()
	photos, total, err := fx.photos.ListPhotos(ctx, database.PhotoFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list photos: %v", err)
	}
	if total != 3 {
		t.Errorf("photos.total = %d, want 3", total)
	}
	if len(photos) != 3 {
		t.Errorf("photos returned = %d, want 3", len(photos))
	}
	for _, p := range photos {
		abs, err := fx.store.AbsOriginal(p.FilePath)
		if err != nil {
			t.Errorf("AbsOriginal(%s): %v", p.FilePath, err)
			continue
		}
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("expected file at %s: %v", abs, err)
		}
	}

	subjects, err := fx.subjects.ListSubjects(ctx, database.SubjectQuery{Limit: 50})
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subjects) != 1 || subjects[0].Name != "Alice Example" {
		t.Errorf("subjects = %+v", subjects)
	}

	labels, err := fx.labels.ListLabels(ctx, database.LabelQuery{Limit: 50})
	if err != nil {
		t.Fatalf("list labels: %v", err)
	}
	if len(labels) != 2 {
		t.Errorf("labels = %d, want 2", len(labels))
	}
	for _, l := range labels {
		if l.Name == "Nature" && l.PhotoCount != 2 {
			t.Errorf("Nature label photo_count = %d, want 2", l.PhotoCount)
		}
		if l.Name == "Sunset" && l.PhotoCount != 1 {
			t.Errorf("Sunset label photo_count = %d, want 1", l.PhotoCount)
		}
	}

	albums, err := fx.albums.ListAlbums(ctx, database.AlbumQuery{Limit: 50})
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums) != 1 || albums[0].Title != "Holiday 2024" {
		t.Errorf("albums = %+v", albums)
	}
	if albums[0].PhotoCount != 2 {
		t.Errorf("album photo_count = %d, want 2", albums[0].PhotoCount)
	}

	// Marker should be attached to photo p001's native UID with the
	// subject we migrated.
	var markerCount int
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM markers WHERE subject_uid IS NOT NULL`).Scan(&markerCount); err != nil {
		t.Fatalf("count markers: %v", err)
	}
	if markerCount != 1 {
		t.Errorf("markers with subject = %d, want 1", markerCount)
	}
}

// assertReRunNoNewRows asserts the second migration pass created zero
// new rows in any stage (idempotency).
func assertReRunNoNewRows(t *testing.T, report *Report) {
	t.Helper()
	for _, s := range report.Stages {
		if s.Created != 0 {
			t.Errorf("re-run stage %s: Created = %d, want 0", s.Stage, s.Created)
		}
	}
}
