//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TestSmartAlbumUserDeleteSurvives proves that deleting the user who
// created a smart album does not abort with a FK violation and leaves
// the smart album intact (created_by_user_uid goes to NULL, surfaced
// as an empty string by the scanner). Regression test for migration 043.
func TestSmartAlbumUserDeleteSurvives(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	users := NewUserRepository(pool)
	smarts := NewSmartAlbumRepository(pool)

	u := makeUser("smartowner", auth.RoleAdmin)
	if err := users.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	album := &database.SmartAlbum{
		Name:             "Favourites",
		Filters:          map[string]any{"favorite": true},
		CreatedByUserUID: u.UID,
	}
	if err := smarts.CreateSmartAlbum(ctx, album); err != nil {
		t.Fatalf("CreateSmartAlbum: %v", err)
	}

	if err := users.DeleteUser(ctx, u.UID); err != nil {
		t.Fatalf("DeleteUser should succeed when author has a saved search: %v", err)
	}

	got, err := smarts.GetSmartAlbum(ctx, album.UID)
	if err != nil {
		t.Fatalf("GetSmartAlbum after user delete: %v", err)
	}
	if got.UID != album.UID {
		t.Errorf("UID = %q, want %q", got.UID, album.UID)
	}
	if got.CreatedByUserUID != "" {
		t.Errorf("CreatedByUserUID should be cleared, got %q", got.CreatedByUserUID)
	}
	if got.Name != "Favourites" {
		t.Errorf("Name = %q, want %q", got.Name, "Favourites")
	}
}

// TestShareLinkUserDeleteSurvives proves that deleting the user who
// minted a share link does not abort with a FK violation and leaves
// the share link intact. Regression test for migration 043.
func TestShareLinkUserDeleteSurvives(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()

	users := NewUserRepository(pool)
	albums := NewAlbumRepository(pool)
	shares := NewShareLinkRepository(pool)

	u := makeUser("shareowner2", auth.RoleAdmin)
	if err := users.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	a := &database.Album{Title: "Holiday", Description: "Test album"}
	if err := albums.CreateAlbum(ctx, a); err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}

	link := &database.ShareLink{
		Slug:             "holiday",
		AlbumUID:         a.UID,
		CreatedByUserUID: u.UID,
	}
	if err := shares.CreateShareLink(ctx, link); err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}

	if err := users.DeleteUser(ctx, u.UID); err != nil {
		t.Fatalf("DeleteUser should succeed when author has a share link: %v", err)
	}

	got, err := shares.GetShareLink(ctx, "holiday")
	if err != nil {
		t.Fatalf("GetShareLink after user delete: %v", err)
	}
	if got.Slug != "holiday" {
		t.Errorf("Slug = %q, want %q", got.Slug, "holiday")
	}
	if got.CreatedByUserUID != "" {
		t.Errorf("CreatedByUserUID should be cleared, got %q", got.CreatedByUserUID)
	}
	if got.AlbumUID != a.UID {
		t.Errorf("AlbumUID = %q, want %q", got.AlbumUID, a.UID)
	}

	// Sanity: GetShareLink should report ErrNotFound for an unrelated slug
	// so the test does not silently fall back to the wrong sentinel.
	if _, err := shares.GetShareLink(ctx, "no-such-slug"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing slug, got %v", err)
	}
}
