//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// shareTestSetup builds the album + user a share link FK needs and
// returns the live ShareLinkRepository. The cleanup func tears the
// shared test container down.
func shareTestSetup(t *testing.T) (*ShareLinkRepository, string, string, func()) {
	t.Helper()
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return nil, "", "", func() {}
	}
	ctx := context.Background()

	userRepo := NewUserRepository(pool)
	u := makeUser("shareowner", "admin")
	if err := userRepo.CreateUser(ctx, u); err != nil {
		cleanup()
		t.Fatalf("CreateUser: %v", err)
	}

	albumRepo := NewAlbumRepository(pool)
	a := &database.Album{Title: "Family Trip", Description: "Test"}
	if err := albumRepo.CreateAlbum(ctx, a); err != nil {
		cleanup()
		t.Fatalf("CreateAlbum: %v", err)
	}

	return NewShareLinkRepository(pool), a.UID, u.UID, cleanup
}

func TestShareLinkRepository_CreateGetListDelete(t *testing.T) {
	repo, albumUID, userUID, cleanup := shareTestSetup(t)
	if repo == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	exp := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
	link := &database.ShareLink{
		Slug:             "family-trip",
		AlbumUID:         albumUID,
		PasswordHash:     "$2a$12$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN.OP",
		ExpiresAt:        &exp,
		CreatedByUserUID: userUID,
	}
	if err := repo.CreateShareLink(ctx, link); err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	if link.CreatedAt.IsZero() {
		t.Errorf("CreatedAt was not populated")
	}

	got, err := repo.GetShareLink(ctx, "family-trip")
	if err != nil {
		t.Fatalf("GetShareLink: %v", err)
	}
	if got.AlbumUID != albumUID {
		t.Errorf("AlbumUID = %q, want %q", got.AlbumUID, albumUID)
	}
	if !got.HasPassword() {
		t.Errorf("HasPassword should be true")
	}
	if got.ExpiresAt == nil {
		t.Fatalf("ExpiresAt was not loaded")
	}

	// List
	list, err := repo.ListShareLinksForAlbum(ctx, albumUID)
	if err != nil {
		t.Fatalf("ListShareLinksForAlbum: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 link, got %d", len(list))
	}

	// Slug collision
	dup := &database.ShareLink{
		Slug:             "family-trip",
		AlbumUID:         albumUID,
		CreatedByUserUID: userUID,
	}
	if err := repo.CreateShareLink(ctx, dup); !errors.Is(err, database.ErrShareLinkSlugTaken) {
		t.Errorf("expected ErrShareLinkSlugTaken, got %v", err)
	}

	// Invalid slug rejected before DB hit.
	bad := &database.ShareLink{
		Slug:             "Bad Slug!",
		AlbumUID:         albumUID,
		CreatedByUserUID: userUID,
	}
	if err := repo.CreateShareLink(ctx, bad); !errors.Is(err, database.ErrShareLinkInvalidSlug) {
		t.Errorf("expected ErrShareLinkInvalidSlug, got %v", err)
	}

	// Delete
	if err := repo.DeleteShareLink(ctx, "family-trip"); err != nil {
		t.Fatalf("DeleteShareLink: %v", err)
	}
	if _, err := repo.GetShareLink(ctx, "family-trip"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	if err := repo.DeleteShareLink(ctx, "family-trip"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("second delete should return ErrNotFound, got %v", err)
	}
}

func TestShareLinkRepository_CascadeOnAlbumDelete(t *testing.T) {
	repo, albumUID, userUID, cleanup := shareTestSetup(t)
	if repo == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	link := &database.ShareLink{
		Slug:             "cascade-link",
		AlbumUID:         albumUID,
		CreatedByUserUID: userUID,
	}
	if err := repo.CreateShareLink(ctx, link); err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}

	albumRepo := NewAlbumRepository(repo.pool)
	if err := albumRepo.DeleteAlbum(ctx, albumUID); err != nil {
		t.Fatalf("DeleteAlbum: %v", err)
	}
	if _, err := repo.GetShareLink(ctx, "cascade-link"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected share link to be cascade-deleted, got %v", err)
	}
}
