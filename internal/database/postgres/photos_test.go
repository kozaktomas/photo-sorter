//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// makePhoto builds a photo struct with realistic defaults; tests override
// individual fields to exercise the column they care about.
func makePhoto(hash, name string) *database.Photo {
	taken := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	lat := 49.30
	lng := 16.70
	alt := 410.0
	iso := 200
	aperture := 2.8
	focal := 35.0
	return &database.Photo{
		FileHash:        hash,
		FilePath:        "2024/06/" + name,
		FileName:        name,
		FileSize:        1234567,
		FileMime:        "image/jpeg",
		FileWidth:       4032,
		FileHeight:      3024,
		FileOrientation: 1,
		TakenAt:         &taken,
		TakenAtSource:   "exif",
		Title:           "Test Photo",
		Description:     "A photo for tests.",
		Notes:           "noted",
		Lat:             &lat,
		Lng:             &lng,
		Altitude:        &alt,
		CameraMake:      "Canon",
		CameraModel:     "EOS R5",
		LensModel:       "RF 35mm",
		ISO:             &iso,
		Aperture:        &aperture,
		Exposure:        "1/250",
		FocalLength:     &focal,
		Exif: map[string]any{
			"Make":  "Canon",
			"Model": "EOS R5",
			"GPS": map[string]any{
				"lat": 49.30,
				"lng": 16.70,
			},
		},
		Favorite: true,
	}
}

func TestNewPhotoUID(t *testing.T) {
	uid := NewPhotoUID()
	if !strings.HasPrefix(uid, "p") {
		t.Errorf("UID should start with 'p', got %q", uid)
	}
	if len(uid) != 1+photoUIDRandLen {
		t.Errorf("UID length = %d, want %d", len(uid), 1+photoUIDRandLen)
	}
	if strings.ToLower(uid) != uid {
		t.Errorf("UID should be lowercase, got %q", uid)
	}
	// Generated IDs should not collide across 64 draws.
	seen := map[string]bool{uid: true}
	for i := 0; i < 64; i++ {
		next := NewPhotoUID()
		if seen[next] {
			t.Fatalf("collision after %d draws: %q", i, next)
		}
		seen[next] = true
	}
}

func TestPhotoRepository_CreateAndGet(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	p := makePhoto("hash-create-get", "IMG_001.jpg")
	if err := repo.CreatePhoto(ctx, p); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}
	if p.UID == "" {
		t.Fatal("UID was not generated")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Fatal("timestamps not populated by CreatePhoto")
	}

	got, err := repo.GetPhoto(ctx, p.UID)
	if err != nil {
		t.Fatalf("GetPhoto: %v", err)
	}
	if got.FileHash != p.FileHash {
		t.Errorf("FileHash mismatch: got %q want %q", got.FileHash, p.FileHash)
	}
	if got.Title != "Test Photo" {
		t.Errorf("Title mismatch: got %q", got.Title)
	}
	if got.ISO == nil || *got.ISO != 200 {
		t.Errorf("ISO mismatch: got %v", got.ISO)
	}
	if got.Lat == nil || *got.Lat != 49.30 {
		t.Errorf("Lat mismatch: got %v", got.Lat)
	}
	if got.Exif == nil {
		t.Fatal("EXIF should be populated")
	}
	if got.Exif["Make"] != "Canon" {
		t.Errorf("EXIF Make mismatch: got %v", got.Exif["Make"])
	}
	gps, ok := got.Exif["GPS"].(map[string]any)
	if !ok {
		t.Fatalf("EXIF GPS should be a map, got %T", got.Exif["GPS"])
	}
	if gps["lat"] != 49.30 {
		t.Errorf("EXIF GPS lat mismatch: got %v", gps["lat"])
	}
	if !got.Favorite {
		t.Error("Favorite should be true")
	}

	// Missing photo returns ErrNotFound.
	if _, err := repo.GetPhoto(ctx, "does-not-exist"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPhotoRepository_GetPhotoByHash(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	p := makePhoto("hash-byhash", "IMG_002.jpg")
	if err := repo.CreatePhoto(ctx, p); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}

	got, err := repo.GetPhotoByHash(ctx, "hash-byhash")
	if err != nil {
		t.Fatalf("GetPhotoByHash: %v", err)
	}
	if got.UID != p.UID {
		t.Errorf("UID mismatch: got %q want %q", got.UID, p.UID)
	}

	if _, err := repo.GetPhotoByHash(ctx, "nope"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Duplicate hash must trigger a unique violation.
	dup := makePhoto("hash-byhash", "IMG_002_dup.jpg")
	err = repo.CreatePhoto(ctx, dup)
	if err == nil {
		t.Fatal("expected unique violation on duplicate hash, got nil")
	}
	if !IsUniqueViolation(err) {
		t.Errorf("expected unique_violation, got %v", err)
	}
}

func TestPhotoRepository_ListPhotos_FiltersAndSort(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	// Insert three photos with distinct dates and names.
	older := makePhoto("hash-older", "older.jpg")
	olderTaken := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	older.TakenAt = &olderTaken
	older.Title = "alpha alpha"
	if err := repo.CreatePhoto(ctx, older); err != nil {
		t.Fatalf("CreatePhoto older: %v", err)
	}

	middle := makePhoto("hash-middle", "middle.jpg")
	middleTaken := time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC)
	middle.TakenAt = &middleTaken
	middle.Title = "bravo bravo"
	if err := repo.CreatePhoto(ctx, middle); err != nil {
		t.Fatalf("CreatePhoto middle: %v", err)
	}

	newer := makePhoto("hash-newer", "newer.jpg")
	newerTaken := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	newer.TakenAt = &newerTaken
	newer.Title = "charlie keyword"
	if err := repo.CreatePhoto(ctx, newer); err != nil {
		t.Fatalf("CreatePhoto newer: %v", err)
	}

	// Default sort = newest first.
	listed, total, err := repo.ListPhotos(ctx, database.PhotoFilter{})
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(listed) != 3 || listed[0].FileName != "newer.jpg" {
		t.Errorf("default sort failed: %+v", fileNames(listed))
	}

	// Sort by oldest.
	listed, _, err = repo.ListPhotos(ctx, database.PhotoFilter{SortBy: "oldest"})
	if err != nil {
		t.Fatalf("ListPhotos oldest: %v", err)
	}
	if listed[0].FileName != "older.jpg" {
		t.Errorf("oldest sort failed: %+v", fileNames(listed))
	}

	// Sort by name.
	listed, _, err = repo.ListPhotos(ctx, database.PhotoFilter{SortBy: "name"})
	if err != nil {
		t.Fatalf("ListPhotos name: %v", err)
	}
	if listed[0].FileName != "middle.jpg" {
		t.Errorf("name sort failed: %+v", fileNames(listed))
	}

	// Date range filter.
	from := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	listed, total, err = repo.ListPhotos(ctx, database.PhotoFilter{TakenFrom: &from, TakenTo: &to})
	if err != nil {
		t.Fatalf("ListPhotos date range: %v", err)
	}
	if total != 1 || listed[0].FileName != "middle.jpg" {
		t.Errorf("date range filter: got %+v (total=%d), want only middle.jpg", fileNames(listed), total)
	}

	// Search string (matches title).
	listed, total, err = repo.ListPhotos(ctx, database.PhotoFilter{Search: "keyword"})
	if err != nil {
		t.Fatalf("ListPhotos search: %v", err)
	}
	if total != 1 || listed[0].FileName != "newer.jpg" {
		t.Errorf("search filter: got %+v (total=%d), want only newer.jpg", fileNames(listed), total)
	}

	// Search matches file_name too.
	listed, _, err = repo.ListPhotos(ctx, database.PhotoFilter{Search: "older"})
	if err != nil {
		t.Fatalf("ListPhotos search by filename: %v", err)
	}
	if len(listed) != 1 || listed[0].FileName != "older.jpg" {
		t.Errorf("filename search failed: %+v", fileNames(listed))
	}

	// Pagination — Limit + Offset on default newest sort.
	page1, _, err := repo.ListPhotos(ctx, database.PhotoFilter{Limit: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	page2, _, err := repo.ListPhotos(ctx, database.PhotoFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page1) != 2 || len(page2) != 1 {
		t.Errorf("pagination split wrong: p1=%d p2=%d", len(page1), len(page2))
	}
	if page2[0].FileName != "older.jpg" {
		t.Errorf("page 2 should hold the oldest record, got %q", page2[0].FileName)
	}
}

func TestPhotoRepository_ArchivedFilter(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	live := makePhoto("hash-live", "live.jpg")
	if err := repo.CreatePhoto(ctx, live); err != nil {
		t.Fatalf("create live: %v", err)
	}
	archived := makePhoto("hash-arch", "arch.jpg")
	if err := repo.CreatePhoto(ctx, archived); err != nil {
		t.Fatalf("create archived: %v", err)
	}
	if err := repo.ArchivePhoto(ctx, archived.UID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Default behaviour — archived rows excluded.
	defaultList, total, err := repo.ListPhotos(ctx, database.PhotoFilter{})
	if err != nil {
		t.Fatalf("ListPhotos default: %v", err)
	}
	if total != 1 || defaultList[0].UID != live.UID {
		t.Errorf("default filter should exclude archived, got %+v", fileNames(defaultList))
	}

	// Archived = true returns only archived rows.
	yes := true
	archList, total, err := repo.ListPhotos(ctx, database.PhotoFilter{Archived: &yes})
	if err != nil {
		t.Fatalf("ListPhotos archived: %v", err)
	}
	if total != 1 || archList[0].UID != archived.UID {
		t.Errorf("archived=true filter wrong: %+v", fileNames(archList))
	}
	if archList[0].ArchivedAt == nil {
		t.Error("archived photo should have ArchivedAt populated")
	}

	// Archived = false (explicit) behaves like the default.
	no := false
	liveList, _, err := repo.ListPhotos(ctx, database.PhotoFilter{Archived: &no})
	if err != nil {
		t.Fatalf("ListPhotos archived=false: %v", err)
	}
	if len(liveList) != 1 || liveList[0].UID != live.UID {
		t.Errorf("archived=false should match default, got %+v", fileNames(liveList))
	}

	// Restore clears the timestamp.
	if err := repo.RestorePhoto(ctx, archived.UID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restored, err := repo.GetPhoto(ctx, archived.UID)
	if err != nil {
		t.Fatalf("GetPhoto after restore: %v", err)
	}
	if restored.ArchivedAt != nil {
		t.Errorf("ArchivedAt should be nil after restore, got %v", restored.ArchivedAt)
	}

	// Restoring a missing photo returns ErrNotFound.
	if err := repo.RestorePhoto(ctx, "no-such-uid"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("RestorePhoto missing: got %v, want ErrNotFound", err)
	}
}

func TestPhotoRepository_PhotoFiles(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	p := makePhoto("hash-files", "files.jpg")
	if err := repo.CreatePhoto(ctx, p); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}

	primary := &database.PhotoFile{
		PhotoUID:  p.UID,
		FilePath:  "2024/06/files.jpg",
		FileHash:  "hash-files",
		FileSize:  1234,
		FileMime:  "image/jpeg",
		IsPrimary: true,
		Role:      "original",
	}
	if err := repo.AddPhotoFile(ctx, primary); err != nil {
		t.Fatalf("AddPhotoFile primary: %v", err)
	}
	if primary.ID == 0 {
		t.Fatal("primary ID should be populated")
	}

	sidecar := &database.PhotoFile{
		PhotoUID: p.UID,
		FilePath: "2024/06/files.xmp",
		FileHash: "hash-files-xmp",
		FileSize: 256,
		FileMime: "application/xml",
		Role:     "sidecar",
	}
	if err := repo.AddPhotoFile(ctx, sidecar); err != nil {
		t.Fatalf("AddPhotoFile sidecar: %v", err)
	}

	files, err := repo.ListPhotoFiles(ctx, p.UID)
	if err != nil {
		t.Fatalf("ListPhotoFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if !files[0].IsPrimary {
		t.Errorf("primary file should sort first, got %+v", files[0])
	}

	// Same (photo_uid, file_path) is rejected by the unique index.
	dup := &database.PhotoFile{
		PhotoUID: p.UID,
		FilePath: primary.FilePath,
		FileHash: "other-hash",
		FileSize: 9999,
		FileMime: "image/jpeg",
	}
	err = repo.AddPhotoFile(ctx, dup)
	if err == nil {
		t.Fatal("expected unique violation on duplicate (photo_uid, file_path)")
	}
	if !IsUniqueViolation(err) {
		t.Errorf("expected unique_violation, got %v", err)
	}

	// DeletePhotoFile removes only the matching row.
	if err := repo.DeletePhotoFile(ctx, p.UID, sidecar.FilePath); err != nil {
		t.Fatalf("DeletePhotoFile: %v", err)
	}
	files, err = repo.ListPhotoFiles(ctx, p.UID)
	if err != nil {
		t.Fatalf("ListPhotoFiles after delete: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file after delete, got %d", len(files))
	}

	// Deleting a non-existent file returns ErrNotFound.
	if err := repo.DeletePhotoFile(ctx, p.UID, "missing.jpg"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("DeletePhotoFile missing: got %v, want ErrNotFound", err)
	}

	// Hard-deleting the photo cascades to remaining photo_files.
	if err := repo.DeletePhoto(ctx, p.UID); err != nil {
		t.Fatalf("DeletePhoto: %v", err)
	}
	files, err = repo.ListPhotoFiles(ctx, p.UID)
	if err != nil {
		t.Fatalf("ListPhotoFiles after photo delete: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files after cascade, got %d", len(files))
	}

	// Deleting it a second time returns ErrNotFound.
	if err := repo.DeletePhoto(ctx, p.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("DeletePhoto missing: got %v, want ErrNotFound", err)
	}
}

func TestPhotoRepository_UpdatePhoto(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	p := makePhoto("hash-update", "update.jpg")
	if err := repo.CreatePhoto(ctx, p); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}
	originalUpdated := p.UpdatedAt

	p.Title = "Updated title"
	p.Description = "Updated description"
	newIso := 800
	p.ISO = &newIso
	// Small wait to ensure updated_at moves forward on the database side.
	time.Sleep(10 * time.Millisecond)

	if err := repo.UpdatePhoto(ctx, p); err != nil {
		t.Fatalf("UpdatePhoto: %v", err)
	}
	if !p.UpdatedAt.After(originalUpdated) {
		t.Errorf("UpdatedAt should advance: before=%v after=%v", originalUpdated, p.UpdatedAt)
	}

	got, err := repo.GetPhoto(ctx, p.UID)
	if err != nil {
		t.Fatalf("GetPhoto after update: %v", err)
	}
	if got.Title != "Updated title" {
		t.Errorf("Title not updated: %q", got.Title)
	}
	if got.ISO == nil || *got.ISO != 800 {
		t.Errorf("ISO not updated: %v", got.ISO)
	}

	// Updating a missing photo returns ErrNotFound.
	p.UID = "missing-uid"
	if err := repo.UpdatePhoto(ctx, p); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("UpdatePhoto missing: got %v, want ErrNotFound", err)
	}
}

// fileNames returns the file_name field of each photo — handy when an
// assertion fails and we want to print what the repo actually returned.
func fileNames(photos []database.Photo) []string {
	out := make([]string, len(photos))
	for i, p := range photos {
		out[i] = p.FileName
	}
	return out
}
