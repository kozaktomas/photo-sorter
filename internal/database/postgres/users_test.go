//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// makeUser builds a UserWithSecret with sensible defaults; tests override
// individual fields as needed.
func makeUser(username string, role string) *database.UserWithSecret {
	return &database.UserWithSecret{
		User: database.User{
			Username:    username,
			DisplayName: strings.ToUpper(username[:1]) + username[1:],
			Email:       username + "@example.com",
			Role:        role,
		},
		PasswordHash: "$2a$12$abcdefghijklmnopqrstuv.placeholderhashvaluelongenough",
	}
}

func TestNewUserUID(t *testing.T) {
	uid := NewUserUID()
	if !strings.HasPrefix(uid, "u") {
		t.Errorf("UID should start with 'u', got %q", uid)
	}
	if len(uid) != 1+userUIDRandLen {
		t.Errorf("UID length = %d, want %d", len(uid), 1+userUIDRandLen)
	}
	if strings.ToLower(uid) != uid {
		t.Errorf("UID should be lowercase, got %q", uid)
	}
	seen := map[string]bool{uid: true}
	for i := 0; i < 64; i++ {
		next := NewUserUID()
		if seen[next] {
			t.Fatalf("collision after %d draws: %q", i, next)
		}
		seen[next] = true
	}
}

func TestHashPassword_RoundTrip(t *testing.T) {
	const plain = "super-secret-pw-123"
	hash, err := auth.HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == plain {
		t.Fatalf("hash looks wrong: %q", hash)
	}
	if !auth.CheckPassword(plain, hash) {
		t.Error("CheckPassword should accept the correct password")
	}
	if auth.CheckPassword("wrong-attempt", hash) {
		t.Error("CheckPassword should reject the wrong password")
	}
	if auth.CheckPassword("", hash) {
		t.Error("CheckPassword should reject an empty attempt")
	}
	if auth.CheckPassword(plain, "") {
		t.Error("CheckPassword should reject an empty hash")
	}
}

func TestHashPassword_EmptyRejected(t *testing.T) {
	if _, err := auth.HashPassword(""); err == nil {
		t.Error("HashPassword should reject empty password")
	}
}

func TestIsValidRole(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{auth.RoleAdmin, true},
		{auth.RoleEditor, true},
		{auth.RoleViewer, true},
		{"", false},
		{"superuser", false},
		{"ADMIN", false},
	}
	for _, c := range cases {
		if got := auth.IsValidRole(c.role); got != c.want {
			t.Errorf("IsValidRole(%q) = %v, want %v", c.role, got, c.want)
		}
	}
}

func TestHasWriteAccess(t *testing.T) {
	if !auth.HasWriteAccess(auth.RoleAdmin) {
		t.Error("admin should have write access")
	}
	if !auth.HasWriteAccess(auth.RoleEditor) {
		t.Error("editor should have write access")
	}
	if auth.HasWriteAccess(auth.RoleViewer) {
		t.Error("viewer should NOT have write access")
	}
	if auth.HasWriteAccess("") {
		t.Error("empty role should NOT have write access")
	}
}

func TestIsAdmin(t *testing.T) {
	if !auth.IsAdmin(auth.RoleAdmin) {
		t.Error("admin should be admin")
	}
	if auth.IsAdmin(auth.RoleEditor) {
		t.Error("editor should not be admin")
	}
}

func TestUserRepository_CreateAndGetByUsername(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	u := makeUser("alice", auth.RoleAdmin)
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.UID == "" || !strings.HasPrefix(u.UID, "u") {
		t.Errorf("UID not populated, got %q", u.UID)
	}
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Error("timestamps not populated")
	}

	// GetUserByUsername returns the secret.
	got, err := repo.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("Username = %q, want alice", got.Username)
	}
	if got.PasswordHash == "" {
		t.Error("PasswordHash should be populated")
	}
	if got.Role != auth.RoleAdmin {
		t.Errorf("Role = %q, want admin", got.Role)
	}

	// GetUser by UID does not expose the hash.
	plain, err := repo.GetUser(ctx, u.UID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if plain.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", plain.Email)
	}
}

func TestUserRepository_GetUser_NotFound(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	if _, err := repo.GetUser(ctx, "u-does-not-exist"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetUserByUsername(ctx, "nobody"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_UsernameUniqueness(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	first := makeUser("dup", auth.RoleViewer)
	if err := repo.CreateUser(ctx, first); err != nil {
		t.Fatalf("CreateUser first: %v", err)
	}

	second := makeUser("dup", auth.RoleEditor)
	err := repo.CreateUser(ctx, second)
	if !errors.Is(err, database.ErrUsernameTaken) {
		t.Errorf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestUserRepository_DisableAndTouchLastLogin(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	u := makeUser("bob", auth.RoleEditor)
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Initial: not disabled, no last login.
	before, err := repo.GetUser(ctx, u.UID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if before.Disabled {
		t.Error("user should not be disabled initially")
	}
	if before.LastLoginAt != nil {
		t.Errorf("LastLoginAt should be nil, got %v", before.LastLoginAt)
	}

	// SetDisabled flips the column.
	if err := repo.SetDisabled(ctx, u.UID, true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	after, err := repo.GetUser(ctx, u.UID)
	if err != nil {
		t.Fatalf("GetUser after disable: %v", err)
	}
	if !after.Disabled {
		t.Error("Disabled flag not updated")
	}

	// TouchLastLogin sets the timestamp.
	beforeTouch := time.Now()
	if err := repo.TouchLastLogin(ctx, u.UID); err != nil {
		t.Fatalf("TouchLastLogin: %v", err)
	}
	touched, err := repo.GetUser(ctx, u.UID)
	if err != nil {
		t.Fatalf("GetUser after touch: %v", err)
	}
	if touched.LastLoginAt == nil {
		t.Fatal("LastLoginAt should be populated after TouchLastLogin")
	}
	if touched.LastLoginAt.Before(beforeTouch.Add(-time.Second)) {
		t.Errorf("LastLoginAt %v is suspiciously old vs %v", touched.LastLoginAt, beforeTouch)
	}

	// SetDisabled on a non-existent user returns ErrNotFound.
	if err := repo.SetDisabled(ctx, "u-nope", true); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("SetDisabled missing user: expected ErrNotFound, got %v", err)
	}
	if err := repo.TouchLastLogin(ctx, "u-nope"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("TouchLastLogin missing user: expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_SetPassword(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	u := makeUser("carol", auth.RoleAdmin)
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newHash, err := auth.HashPassword("new-pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := repo.SetPassword(ctx, u.UID, newHash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	got, err := repo.GetUserByUsername(ctx, "carol")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.PasswordHash != newHash {
		t.Errorf("PasswordHash not updated; got %q, want %q", got.PasswordHash, newHash)
	}
	if !auth.CheckPassword("new-pw", got.PasswordHash) {
		t.Error("Updated hash should verify against the new plaintext")
	}

	if err := repo.SetPassword(ctx, "u-nope", newHash); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("SetPassword missing user: expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_UpdateUser(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	u := makeUser("dave", auth.RoleViewer)
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	plain := u.User
	plain.DisplayName = "David Renamed"
	plain.Role = auth.RoleEditor
	plain.Email = "david@example.com"

	if err := repo.UpdateUser(ctx, &plain); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, err := repo.GetUser(ctx, u.UID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.DisplayName != "David Renamed" || got.Role != auth.RoleEditor || got.Email != "david@example.com" {
		t.Errorf("update not reflected: %+v", got)
	}

	missing := plain
	missing.UID = "u-nope"
	if err := repo.UpdateUser(ctx, &missing); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("UpdateUser missing: expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_DeleteUser(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	u := makeUser("erin", auth.RoleEditor)
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := repo.DeleteUser(ctx, u.UID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := repo.GetUser(ctx, u.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("user should be gone: %v", err)
	}
	if err := repo.DeleteUser(ctx, u.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("DeleteUser second call: expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_ListAndCount(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)

	n0, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers initial: %v", err)
	}
	if n0 != 0 {
		t.Errorf("initial user count = %d, want 0", n0)
	}

	for _, name := range []string{"frank", "grace", "heidi"} {
		if err := repo.CreateUser(ctx, makeUser(name, auth.RoleViewer)); err != nil {
			t.Fatalf("CreateUser %s: %v", name, err)
		}
	}

	n, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 3 {
		t.Errorf("CountUsers = %d, want 3", n)
	}

	users, err := repo.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("ListUsers len = %d, want 3", len(users))
	}
	// Must be username-sorted.
	if users[0].Username != "frank" || users[1].Username != "grace" || users[2].Username != "heidi" {
		t.Errorf("ListUsers not username-sorted: %v %v %v",
			users[0].Username, users[1].Username, users[2].Username)
	}
}

func TestBootstrapAdmin(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewUserRepository(pool)
	cfg := config.Config{}

	// 1. Missing env vars + empty users table: should log a WARN and no-op.
	t.Setenv("BOOTSTRAP_ADMIN_USERNAME", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
	if err := auth.BootstrapAdmin(ctx, repo, repo, cfg); err != nil {
		t.Fatalf("BootstrapAdmin (no env): %v", err)
	}
	n, _ := repo.CountUsers(ctx)
	if n != 0 {
		t.Errorf("no env should not create user, count = %d", n)
	}

	// 2. With env vars + empty users table: creates one admin user.
	if err := os.Setenv("BOOTSTRAP_ADMIN_USERNAME", "root"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "ChangeMe!2024"); err != nil {
		t.Fatal(err)
	}
	if err := auth.BootstrapAdmin(ctx, repo, repo, cfg); err != nil {
		t.Fatalf("BootstrapAdmin (with env, empty table): %v", err)
	}
	n, _ = repo.CountUsers(ctx)
	if n != 1 {
		t.Fatalf("expected 1 user after bootstrap, got %d", n)
	}
	got, err := repo.GetUserByUsername(ctx, "root")
	if err != nil {
		t.Fatalf("GetUserByUsername root: %v", err)
	}
	if got.Role != auth.RoleAdmin {
		t.Errorf("bootstrap user role = %q, want admin", got.Role)
	}
	if !auth.CheckPassword("ChangeMe!2024", got.PasswordHash) {
		t.Error("bootstrap password hash should verify")
	}

	// 3. Idempotent: second call with users already existing is a no-op.
	if err := auth.BootstrapAdmin(ctx, repo, repo, cfg); err != nil {
		t.Fatalf("BootstrapAdmin (second call): %v", err)
	}
	n, _ = repo.CountUsers(ctx)
	if n != 1 {
		t.Errorf("idempotency: expected 1 user, got %d", n)
	}

	// 4. Even with different env vars, still a no-op once a user exists.
	t.Setenv("BOOTSTRAP_ADMIN_USERNAME", "different")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "alsoDifferent")
	if err := auth.BootstrapAdmin(ctx, repo, repo, cfg); err != nil {
		t.Fatalf("BootstrapAdmin (different env, existing user): %v", err)
	}
	n, _ = repo.CountUsers(ctx)
	if n != 1 {
		t.Errorf("expected 1 user, got %d", n)
	}
	if _, err := repo.GetUserByUsername(ctx, "different"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("'different' user should not have been created, got err %v", err)
	}
}
