//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

func TestNewMarkerUID(t *testing.T) {
	uid := NewMarkerUID()
	if !strings.HasPrefix(uid, "m") {
		t.Errorf("UID should start with 'm', got %q", uid)
	}
	if len(uid) != 1+markerUIDRandLen {
		t.Errorf("UID length = %d, want %d", len(uid), 1+markerUIDRandLen)
	}
	if strings.ToLower(uid) != uid {
		t.Errorf("UID should be lowercase, got %q", uid)
	}
	seen := map[string]bool{uid: true}
	for i := 0; i < 64; i++ {
		next := NewMarkerUID()
		if seen[next] {
			t.Fatalf("collision after %d draws: %q", i, next)
		}
		seen[next] = true
	}
}

// makeMarker builds a marker struct with sensible defaults for the given
// photo, so tests can override only the fields they care about.
func makeMarker(photoUID string) *database.Marker {
	return &database.Marker{
		PhotoUID: photoUID,
		Type:     "face",
		X:        0.1, Y: 0.2, W: 0.3, H: 0.4,
		Score: 90,
	}
}

func TestMarkerRepository_CreateGetListForPhoto(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewMarkerRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-marker-1")

	m := makeMarker(p)
	if err := repo.CreateMarker(ctx, m); err != nil {
		t.Fatalf("create marker: %v", err)
	}
	if m.UID == "" || !strings.HasPrefix(m.UID, "m") {
		t.Errorf("UID not populated, got %q", m.UID)
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		t.Error("timestamps not populated")
	}

	got, err := repo.GetMarker(ctx, m.UID)
	if err != nil {
		t.Fatalf("get marker: %v", err)
	}
	if got.PhotoUID != p {
		t.Errorf("PhotoUID = %q, want %q", got.PhotoUID, p)
	}
	if got.SubjectUID != "" {
		t.Errorf("SubjectUID = %q, want empty", got.SubjectUID)
	}
	if got.Score != 90 {
		t.Errorf("Score = %d, want 90", got.Score)
	}
	if got.X != 0.1 || got.Y != 0.2 || got.W != 0.3 || got.H != 0.4 {
		t.Errorf("coords mismatch: %+v", got)
	}

	// ListMarkersForPhoto returns the marker.
	markers, err := repo.ListMarkersForPhoto(ctx, p)
	if err != nil {
		t.Fatalf("list for photo: %v", err)
	}
	if len(markers) != 1 || markers[0].UID != m.UID {
		t.Errorf("list for photo wrong: %+v", markers)
	}

	// GetMarker missing returns ErrNotFound.
	if _, err := repo.GetMarker(ctx, "no-such-uid"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("get missing: got %v, want ErrNotFound", err)
	}
}

func TestMarkerRepository_AssignAndUnassignSubject(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	mRepo := NewMarkerRepository(pool)
	sRepo := NewSubjectRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-marker-assign")
	subj, err := sRepo.EnsureSubject(ctx, "Eva", "person")
	if err != nil {
		t.Fatalf("ensure subject: %v", err)
	}

	m := makeMarker(p)
	if err := mRepo.CreateMarker(ctx, m); err != nil {
		t.Fatalf("create marker: %v", err)
	}

	// Assign.
	if err := mRepo.AssignSubject(ctx, m.UID, subj.UID); err != nil {
		t.Fatalf("assign subject: %v", err)
	}
	got, err := mRepo.GetMarker(ctx, m.UID)
	if err != nil {
		t.Fatalf("get after assign: %v", err)
	}
	if got.SubjectUID != subj.UID {
		t.Errorf("SubjectUID = %q, want %q", got.SubjectUID, subj.UID)
	}

	// ListMarkersForSubject returns the marker.
	markers, err := mRepo.ListMarkersForSubject(ctx, subj.UID, 0, 0)
	if err != nil {
		t.Fatalf("list for subject: %v", err)
	}
	if len(markers) != 1 || markers[0].UID != m.UID {
		t.Errorf("list for subject wrong: %+v", markers)
	}

	// Subject's PhotoCount + FaceCount populated.
	got2, err := sRepo.GetSubject(ctx, subj.UID)
	if err != nil {
		t.Fatalf("get subject: %v", err)
	}
	if got2.PhotoCount != 1 {
		t.Errorf("PhotoCount = %d, want 1", got2.PhotoCount)
	}
	if got2.FaceCount != 1 {
		t.Errorf("FaceCount = %d, want 1", got2.FaceCount)
	}

	// Unassign.
	if err := mRepo.UnassignSubject(ctx, m.UID); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	got, err = mRepo.GetMarker(ctx, m.UID)
	if err != nil {
		t.Fatalf("get after unassign: %v", err)
	}
	if got.SubjectUID != "" {
		t.Errorf("SubjectUID after unassign = %q, want empty", got.SubjectUID)
	}

	// AssignSubject on a missing marker returns ErrNotFound.
	if err := mRepo.AssignSubject(ctx, "no-such-marker", subj.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("assign missing marker: got %v, want ErrNotFound", err)
	}
	if err := mRepo.UnassignSubject(ctx, "no-such-marker"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("unassign missing marker: got %v, want ErrNotFound", err)
	}
}

func TestMarkerRepository_DeleteSubject_NullifiesMarkers(t *testing.T) {
	// Verifies the FK ON DELETE SET NULL clause from migration 032: deleting
	// a subject does NOT cascade to its markers; instead subject_uid is
	// cleared and the marker rows remain.
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	mRepo := NewMarkerRepository(pool)
	sRepo := NewSubjectRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-marker-fk")
	subj, err := sRepo.EnsureSubject(ctx, "ToBeDeleted", "person")
	if err != nil {
		t.Fatalf("ensure subject: %v", err)
	}
	m := makeMarker(p)
	m.SubjectUID = subj.UID
	if err := mRepo.CreateMarker(ctx, m); err != nil {
		t.Fatalf("create marker: %v", err)
	}

	if err := sRepo.DeleteSubject(ctx, subj.UID); err != nil {
		t.Fatalf("delete subject: %v", err)
	}

	got, err := mRepo.GetMarker(ctx, m.UID)
	if err != nil {
		t.Fatalf("get marker after subject delete: %v", err)
	}
	if got.SubjectUID != "" {
		t.Errorf("SubjectUID after subject delete = %q, want empty (FK SET NULL)",
			got.SubjectUID)
	}

	// Marker should still exist in the database.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM markers WHERE uid = $1`, m.UID).Scan(&count); err != nil {
		t.Fatalf("count markers: %v", err)
	}
	if count != 1 {
		t.Errorf("expected marker to still exist, got count = %d", count)
	}
}

func TestMarkerRepository_UpdateMarker(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewMarkerRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-marker-update")
	m := makeMarker(p)
	if err := repo.CreateMarker(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}

	m.X = 0.5
	m.Y = 0.5
	m.W = 0.25
	m.H = 0.25
	m.Score = 75
	m.Reviewed = true
	if err := repo.UpdateMarker(ctx, m); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.GetMarker(ctx, m.UID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.X != 0.5 || got.Y != 0.5 || got.W != 0.25 || got.H != 0.25 {
		t.Errorf("coords not updated: %+v", got)
	}
	if got.Score != 75 {
		t.Errorf("Score = %d, want 75", got.Score)
	}
	if !got.Reviewed {
		t.Error("Reviewed not updated")
	}

	// Missing marker.
	missing := *m
	missing.UID = "no-such-marker"
	if err := repo.UpdateMarker(ctx, &missing); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("update missing: got %v, want ErrNotFound", err)
	}
}

func TestMarkerRepository_SetInvalid_ExcludesFromCounts(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	mRepo := NewMarkerRepository(pool)
	sRepo := NewSubjectRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-marker-invalid")
	subj, err := sRepo.EnsureSubject(ctx, "InvalidTest", "person")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	m := makeMarker(p)
	m.SubjectUID = subj.UID
	if err := mRepo.CreateMarker(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Before flagging invalid: counts include the marker.
	got, err := sRepo.GetSubject(ctx, subj.UID)
	if err != nil {
		t.Fatalf("get subject before invalid: %v", err)
	}
	if got.PhotoCount != 1 || got.FaceCount != 1 {
		t.Errorf("pre-invalid counts wrong: PhotoCount=%d FaceCount=%d, want 1/1",
			got.PhotoCount, got.FaceCount)
	}

	if err := mRepo.SetInvalid(ctx, m.UID, true); err != nil {
		t.Fatalf("set invalid: %v", err)
	}
	gotMarker, err := mRepo.GetMarker(ctx, m.UID)
	if err != nil {
		t.Fatalf("get marker after invalid: %v", err)
	}
	if !gotMarker.Invalid {
		t.Error("Invalid not set on marker")
	}

	// After flagging invalid: PhotoCount/FaceCount drop to 0.
	got, err = sRepo.GetSubject(ctx, subj.UID)
	if err != nil {
		t.Fatalf("get subject after invalid: %v", err)
	}
	if got.PhotoCount != 0 || got.FaceCount != 0 {
		t.Errorf("post-invalid counts wrong: PhotoCount=%d FaceCount=%d, want 0/0",
			got.PhotoCount, got.FaceCount)
	}

	// Toggling invalid back to false restores the count.
	if err := mRepo.SetInvalid(ctx, m.UID, false); err != nil {
		t.Fatalf("clear invalid: %v", err)
	}
	got, err = sRepo.GetSubject(ctx, subj.UID)
	if err != nil {
		t.Fatalf("get subject after clearing invalid: %v", err)
	}
	if got.PhotoCount != 1 || got.FaceCount != 1 {
		t.Errorf("re-validated counts wrong: PhotoCount=%d FaceCount=%d, want 1/1",
			got.PhotoCount, got.FaceCount)
	}

	// SetInvalid on a missing marker returns ErrNotFound.
	if err := mRepo.SetInvalid(ctx, "no-such-marker", true); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("set invalid missing: got %v, want ErrNotFound", err)
	}
}

func TestMarkerRepository_DeleteMarker(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewMarkerRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-marker-delete")
	m := makeMarker(p)
	if err := repo.CreateMarker(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.DeleteMarker(ctx, m.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetMarker(ctx, m.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("get after delete: got %v, want ErrNotFound", err)
	}

	// Deleting a missing marker returns ErrNotFound.
	if err := repo.DeleteMarker(ctx, "no-such-marker"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("delete missing: got %v, want ErrNotFound", err)
	}
}

func TestMarkerRepository_ListMarkersForSubject_Pagination(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	mRepo := NewMarkerRepository(pool)
	sRepo := NewSubjectRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p1 := createTestPhoto(t, photoRepo, "hash-pag-1")
	p2 := createTestPhoto(t, photoRepo, "hash-pag-2")
	p3 := createTestPhoto(t, photoRepo, "hash-pag-3")
	subj, err := sRepo.EnsureSubject(ctx, "Pagination", "person")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, p := range []string{p1, p2, p3} {
		m := makeMarker(p)
		m.SubjectUID = subj.UID
		if err := mRepo.CreateMarker(ctx, m); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	page1, err := mRepo.ListMarkersForSubject(ctx, subj.UID, 2, 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	page2, err := mRepo.ListMarkersForSubject(ctx, subj.UID, 2, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page1) != 2 || len(page2) != 1 {
		t.Errorf("pagination split wrong: p1=%d p2=%d", len(page1), len(page2))
	}
}
