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
			notes VARCHAR(2048) NULL
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
// files + the details table notes for photo #1.
func insertPhotosFilesDetails(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO photos (id, photo_uid, taken_at, photo_title, photo_caption,
		                     photo_lat, photo_lng, photo_iso, photo_f_number, photo_exposure,
		                     photo_focal_length, photo_favorite, photo_private,
		                     camera_id, lens_id)
		 VALUES
			(1, 'p001', '2024-06-15 12:00:00', 'Sunset', 'A nice sunset',
			 49.5, 16.5, 100, 2.8, '1/200', 35, 1, 0, 1, 1),
			(2, 'p002', '2024-07-04 09:30:00', 'Park walk', '',
			 NULL, NULL, NULL, NULL, '', NULL, 0, 0, 1, NULL),
			(3, 'p003', NULL, 'No date', '',
			 NULL, NULL, NULL, NULL, '', NULL, 0, 1, NULL, NULL)`,
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
		`INSERT INTO details (photo_id, notes) VALUES (1, 'shot at golden hour')`); err != nil {
		return fmt.Errorf("insert details: %w", err)
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
		"2024/06/IMG_0001.jpg":     0x01,
		"2024/07/IMG_0002.jpg":     0x02,
		"unknown/IMG_0003.jpg":     0x03,
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

	// Re-run: every stage should now report zero creations because the
	// destination already has the rows.
	report2, err := Run(ctx, buildOptions(fx))
	if err != nil {
		t.Fatalf("re-run migration: %v", err)
	}
	assertReRunNoNewRows(t, report2)
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
		StageSubjects:   1,
		StagePhotos:     3,
		StageLabels:     2,
		"photo_labels":  3,
		StageAlbums:     1,
		"album_photos":  2,
		StageMarkers:    1,
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
