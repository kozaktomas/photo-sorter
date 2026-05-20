//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// makeAlbum builds an album struct with sensible defaults; tests override
// individual fields to exercise the column they care about.
func makeAlbum(title string) *database.Album {
	return &database.Album{
		Title:       title,
		Description: "test album: " + title,
		Type:        "album",
		OrderBy:     "newest",
	}
}

// createTestPhoto inserts a photo into the photos table so the FKs on
// album_photos and albums.cover_photo_uid can be satisfied. Returns the
// generated UID.
func createTestPhoto(t *testing.T, repo *PhotoRepository, hash string) string {
	t.Helper()
	p := makePhoto(hash, hash+".jpg")
	if err := repo.CreatePhoto(context.Background(), p); err != nil {
		t.Fatalf("CreatePhoto %s: %v", hash, err)
	}
	return p.UID
}

func TestNewAlbumUID(t *testing.T) {
	uid := NewAlbumUID()
	if !strings.HasPrefix(uid, "a") {
		t.Errorf("UID should start with 'a', got %q", uid)
	}
	if len(uid) != 1+albumUIDRandLen {
		t.Errorf("UID length = %d, want %d", len(uid), 1+albumUIDRandLen)
	}
	if strings.ToLower(uid) != uid {
		t.Errorf("UID should be lowercase, got %q", uid)
	}
	seen := map[string]bool{uid: true}
	for i := 0; i < 64; i++ {
		next := NewAlbumUID()
		if seen[next] {
			t.Fatalf("collision after %d draws: %q", i, next)
		}
		seen[next] = true
	}
}

func TestSlugifyTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hello World", "hello-world"},
		{"  Trimmed  ", "trimmed"},
		{"Příliš žluťoučký kůň", "prilis-zlutoucky-kun"},
		{"Spaces  and---dashes", "spaces-and-dashes"},
		{"  ---  ", "album"},
		{"", "album"},
		{"123_456!", "123-456"},
	}
	for _, c := range cases {
		got := slugifyTitle(c.in)
		if got != c.want {
			t.Errorf("slugifyTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAlbumRepository_CreateGetUpdateDelete(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewAlbumRepository(pool)

	a := makeAlbum("Summer 2024")
	if err := repo.CreateAlbum(ctx, a); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if a.UID == "" || !strings.HasPrefix(a.UID, "a") {
		t.Errorf("UID not populated, got %q", a.UID)
	}
	if a.Slug != "summer-2024" {
		t.Errorf("Slug = %q, want summer-2024", a.Slug)
	}
	if a.CreatedAt.IsZero() {
		t.Error("CreatedAt not populated")
	}

	got, err := repo.GetAlbum(ctx, a.UID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if got.Title != "Summer 2024" {
		t.Errorf("Title mismatch: %q", got.Title)
	}
	if got.PhotoCount != 0 {
		t.Errorf("PhotoCount = %d, want 0", got.PhotoCount)
	}

	// Lookup by slug works too.
	bySlug, err := repo.GetAlbumBySlug(ctx, "summer-2024")
	if err != nil {
		t.Fatalf("GetAlbumBySlug: %v", err)
	}
	if bySlug.UID != a.UID {
		t.Errorf("GetAlbumBySlug UID mismatch: got %q want %q", bySlug.UID, a.UID)
	}

	// Update fields.
	a.Title = "Summer 2024 (Updated)"
	a.Description = "Best summer ever"
	a.Favorite = true
	if err := repo.UpdateAlbum(ctx, a); err != nil {
		t.Fatalf("UpdateAlbum: %v", err)
	}
	got, err = repo.GetAlbum(ctx, a.UID)
	if err != nil {
		t.Fatalf("GetAlbum after update: %v", err)
	}
	if got.Title != "Summer 2024 (Updated)" {
		t.Errorf("Title not updated: %q", got.Title)
	}
	if !got.Favorite {
		t.Error("Favorite not updated")
	}

	// Delete cascades the album row.
	if err := repo.DeleteAlbum(ctx, a.UID); err != nil {
		t.Fatalf("DeleteAlbum: %v", err)
	}
	if _, err := repo.GetAlbum(ctx, a.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("GetAlbum after delete: got %v, want ErrNotFound", err)
	}

	// Re-deleting returns ErrNotFound.
	if err := repo.DeleteAlbum(ctx, a.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("DeleteAlbum missing: got %v, want ErrNotFound", err)
	}

	// Updating a missing album returns ErrNotFound.
	a.UID = "no-such-album"
	if err := repo.UpdateAlbum(ctx, a); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("UpdateAlbum missing: got %v, want ErrNotFound", err)
	}
}

func TestAlbumRepository_SlugCollision(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewAlbumRepository(pool)

	a1 := makeAlbum("Trip")
	if err := repo.CreateAlbum(ctx, a1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if a1.Slug != "trip" {
		t.Errorf("a1.Slug = %q, want trip", a1.Slug)
	}

	a2 := makeAlbum("Trip")
	if err := repo.CreateAlbum(ctx, a2); err != nil {
		t.Fatalf("create a2: %v", err)
	}
	if a2.Slug != "trip-2" {
		t.Errorf("a2.Slug = %q, want trip-2", a2.Slug)
	}

	a3 := makeAlbum("Trip")
	if err := repo.CreateAlbum(ctx, a3); err != nil {
		t.Fatalf("create a3: %v", err)
	}
	if a3.Slug != "trip-3" {
		t.Errorf("a3.Slug = %q, want trip-3", a3.Slug)
	}

	// Diacritics fold to the same base slug.
	a4 := makeAlbum("TŘÍP")
	if err := repo.CreateAlbum(ctx, a4); err != nil {
		t.Fatalf("create a4: %v", err)
	}
	if a4.Slug != "trip-4" {
		t.Errorf("a4.Slug = %q, want trip-4", a4.Slug)
	}

	// Updating an album to its own slug keeps it unchanged.
	a2.Title = "Trip — renamed"
	a2.Slug = "trip-2"
	if err := repo.UpdateAlbum(ctx, a2); err != nil {
		t.Fatalf("update a2: %v", err)
	}
	if a2.Slug != "trip-2" {
		t.Errorf("a2.Slug after self-update = %q, want trip-2", a2.Slug)
	}
}

func TestAlbumRepository_ListAlbums_FiltersAndSort(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewAlbumRepository(pool)

	older := makeAlbum("Alpha")
	older.Favorite = true
	if err := repo.CreateAlbum(ctx, older); err != nil {
		t.Fatalf("create older: %v", err)
	}
	middle := makeAlbum("Bravo")
	middle.Type = "folder"
	if err := repo.CreateAlbum(ctx, middle); err != nil {
		t.Fatalf("create middle: %v", err)
	}
	newer := makeAlbum("Charlie keyword")
	if err := repo.CreateAlbum(ctx, newer); err != nil {
		t.Fatalf("create newer: %v", err)
	}

	// Default: all visible, newest first.
	listed, err := repo.ListAlbums(ctx, database.AlbumQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 albums, got %d", len(listed))
	}
	if listed[0].Title != "Charlie keyword" {
		t.Errorf("default sort: first = %q, want Charlie keyword", listed[0].Title)
	}

	// Type filter.
	listed, err = repo.ListAlbums(ctx, database.AlbumQuery{Type: "folder"})
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if len(listed) != 1 || listed[0].Title != "Bravo" {
		t.Errorf("type filter wrong: %+v", listed)
	}

	// Favorite filter.
	yes := true
	listed, err = repo.ListAlbums(ctx, database.AlbumQuery{Favorite: &yes})
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	if len(listed) != 1 || listed[0].Title != "Alpha" {
		t.Errorf("favorite filter wrong: %+v", listed)
	}

	// Search filter (matches description too).
	listed, err = repo.ListAlbums(ctx, database.AlbumQuery{Search: "keyword"})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if len(listed) != 1 || listed[0].Title != "Charlie keyword" {
		t.Errorf("search filter wrong: %+v", listed)
	}

	// Sort by title.
	listed, err = repo.ListAlbums(ctx, database.AlbumQuery{SortBy: "title"})
	if err != nil {
		t.Fatalf("list by title: %v", err)
	}
	if len(listed) != 3 || listed[0].Title != "Alpha" {
		t.Errorf("title sort wrong: %+v", listed)
	}

	// Sort by oldest.
	listed, err = repo.ListAlbums(ctx, database.AlbumQuery{SortBy: "oldest"})
	if err != nil {
		t.Fatalf("list oldest: %v", err)
	}
	if listed[0].Title != "Alpha" {
		t.Errorf("oldest sort wrong: %+v", listed)
	}

	// Pagination.
	page1, err := repo.ListAlbums(ctx, database.AlbumQuery{Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	page2, err := repo.ListAlbums(ctx, database.AlbumQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page1) != 2 || len(page2) != 1 {
		t.Errorf("pagination split wrong: p1=%d p2=%d", len(page1), len(page2))
	}
}

func TestAlbumRepository_AddAndRemovePhotos(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	albumRepo := NewAlbumRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p1 := createTestPhoto(t, photoRepo, "hash-a1")
	p2 := createTestPhoto(t, photoRepo, "hash-a2")
	p3 := createTestPhoto(t, photoRepo, "hash-a3")

	a := makeAlbum("Photos test")
	if err := albumRepo.CreateAlbum(ctx, a); err != nil {
		t.Fatalf("create album: %v", err)
	}

	if err := albumRepo.AddPhotos(ctx, a.UID, []string{p1, p2}); err != nil {
		t.Fatalf("add p1+p2: %v", err)
	}

	uids, err := albumRepo.ListAlbumPhotoUIDs(ctx, a.UID)
	if err != nil {
		t.Fatalf("list uids: %v", err)
	}
	if len(uids) != 2 || uids[0] != p1 || uids[1] != p2 {
		t.Errorf("uids order wrong: %v (want %s, %s)", uids, p1, p2)
	}

	// PhotoCount reflects in GetAlbum.
	got, err := albumRepo.GetAlbum(ctx, a.UID)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}
	if got.PhotoCount != 2 {
		t.Errorf("PhotoCount = %d, want 2", got.PhotoCount)
	}

	// Re-adding existing UIDs is a no-op; adding a fresh UID appends at the end.
	if err := albumRepo.AddPhotos(ctx, a.UID, []string{p1, p3}); err != nil {
		t.Fatalf("add p1 again + p3: %v", err)
	}
	uids, err = albumRepo.ListAlbumPhotoUIDs(ctx, a.UID)
	if err != nil {
		t.Fatalf("list uids 2: %v", err)
	}
	if len(uids) != 3 {
		t.Fatalf("expected 3 uids, got %d (%v)", len(uids), uids)
	}
	if uids[0] != p1 || uids[1] != p2 || uids[2] != p3 {
		t.Errorf("order after re-add: %v (want %s, %s, %s)", uids, p1, p2, p3)
	}

	// Remove a single photo.
	if err := albumRepo.RemovePhotos(ctx, a.UID, []string{p2}); err != nil {
		t.Fatalf("remove p2: %v", err)
	}
	uids, err = albumRepo.ListAlbumPhotoUIDs(ctx, a.UID)
	if err != nil {
		t.Fatalf("list uids after remove: %v", err)
	}
	if len(uids) != 2 || uids[0] != p1 || uids[1] != p3 {
		t.Errorf("order after remove: %v", uids)
	}

	// Removing a non-member is a silent no-op.
	if err := albumRepo.RemovePhotos(ctx, a.UID, []string{"no-such-photo"}); err != nil {
		t.Errorf("removing non-member should be silent, got %v", err)
	}
}

func TestAlbumRepository_SetCoverPhoto(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	albumRepo := NewAlbumRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p1 := createTestPhoto(t, photoRepo, "hash-cover1")
	p2 := createTestPhoto(t, photoRepo, "hash-cover2")

	a := makeAlbum("Cover test")
	if err := albumRepo.CreateAlbum(ctx, a); err != nil {
		t.Fatalf("create album: %v", err)
	}
	if err := albumRepo.AddPhotos(ctx, a.UID, []string{p1}); err != nil {
		t.Fatalf("add p1: %v", err)
	}

	// Setting cover to non-member fails with ErrAlbumPhotoNotInAlbum.
	if err := albumRepo.SetCoverPhoto(ctx, a.UID, p2); !errors.Is(err, database.ErrAlbumPhotoNotInAlbum) {
		t.Errorf("set cover to non-member: got %v, want ErrAlbumPhotoNotInAlbum", err)
	}

	// Setting cover to a member succeeds.
	if err := albumRepo.SetCoverPhoto(ctx, a.UID, p1); err != nil {
		t.Fatalf("set cover to p1: %v", err)
	}
	got, err := albumRepo.GetAlbum(ctx, a.UID)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}
	if got.CoverPhotoUID != p1 {
		t.Errorf("CoverPhotoUID = %q, want %q", got.CoverPhotoUID, p1)
	}

	// Removing the cover photo clears CoverPhotoUID via the writer's
	// cleanup pass.
	if err := albumRepo.RemovePhotos(ctx, a.UID, []string{p1}); err != nil {
		t.Fatalf("remove p1: %v", err)
	}
	got, err = albumRepo.GetAlbum(ctx, a.UID)
	if err != nil {
		t.Fatalf("get album after remove: %v", err)
	}
	if got.CoverPhotoUID != "" {
		t.Errorf("CoverPhotoUID after remove = %q, want empty", got.CoverPhotoUID)
	}

	// Setting cover on a missing album returns ErrNotFound.
	if err := albumRepo.SetCoverPhoto(ctx, "no-such-album", p1); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("set cover missing album: got %v, want ErrNotFound", err)
	}
}

func TestAlbumRepository_ListAlbumsForPhoto(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	albumRepo := NewAlbumRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-membership")

	a1 := makeAlbum("First")
	if err := albumRepo.CreateAlbum(ctx, a1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	a2 := makeAlbum("Second")
	if err := albumRepo.CreateAlbum(ctx, a2); err != nil {
		t.Fatalf("create a2: %v", err)
	}
	a3 := makeAlbum("Third — empty for this photo")
	if err := albumRepo.CreateAlbum(ctx, a3); err != nil {
		t.Fatalf("create a3: %v", err)
	}

	if err := albumRepo.AddPhotos(ctx, a1.UID, []string{p}); err != nil {
		t.Fatalf("add to a1: %v", err)
	}
	if err := albumRepo.AddPhotos(ctx, a2.UID, []string{p}); err != nil {
		t.Fatalf("add to a2: %v", err)
	}

	memberships, err := albumRepo.ListAlbumsForPhoto(ctx, p)
	if err != nil {
		t.Fatalf("list memberships: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(memberships))
	}
	got := map[string]bool{}
	for _, m := range memberships {
		got[m.UID] = true
		if m.PhotoCount != 1 {
			t.Errorf("PhotoCount = %d for %q, want 1", m.PhotoCount, m.Title)
		}
	}
	if !got[a1.UID] || !got[a2.UID] {
		t.Errorf("missing expected memberships: %v", memberships)
	}
}
